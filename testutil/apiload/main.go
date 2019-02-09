package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"
)

const (
	outputFileTemplate = "result_%s_%s.json"
	resultChanBuffer   = 4096
)

// Attack specifies a duration and a series of attackers, each with their own
// frequency and targets. Attack is unmarshaled from a JSON definition file.
type Attack struct {
	Duration  int         `json:"duration"`
	Attackers []*Attacker `json:"attackers"`
}

// Attacker defines a list of endpoints for, and a frequency of, attack.
// An optional name will be displayed with error messages.
// An individual Attacker can be delayed, allowing attacks to be layered in
// sections. Frequency is 1/second and endpoints are relative to config.Server.
type Attacker struct {
	Name      string   `json:"name"`
	Frequency int      `json:"frequency"`
	Delay     float32  `json:"delay"`
	Endpoints []string `json:"endpoints"`
}

var Config *config

func main() {
	var err error

	Config, err = loadConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Profiles load from a JSON file. The default profiles are in profiles.json.
	profiles, err := loadProfiles()
	if err != nil {
		log.Fatalf("%v", err)
	}

	if Config.ListAttacks {
		listAttacks(profiles)
		return
	}

	if Config.Attack == "" {
		fmt.Print("No attack specified. Specify an attack with -a.\n\n")
		listAttacks(profiles)
		return
	}

	attackProfile, ok := profiles[Config.Attack]
	if !ok {
		fmt.Printf("Specified attack profile, %s, not found in definitions at %s\n\n", Config.Attack, Config.ProfilesPath)
		listAttacks(profiles)
		return
	}

	err = createResultsDirectory()
	if err != nil {
		log.Fatalf("%v", err)
	}

	// If CPUs are specified, set a max
	if Config.CPUs >= 1 {
		runtime.GOMAXPROCS(Config.CPUs)
		log.Printf("Running on %d CPUs.", Config.CPUs)
	}

	var wg sync.WaitGroup
	resChan := make(chan *vegeta.Result, resultChanBuffer)

	// Duration from profile can be overridden in config or cli argument.
	seconds := attackProfile.Duration
	if Config.Duration > 0 {
		seconds = Config.Duration
	}

	// Perform the load test.
	log.Printf("Beginning %s. Duration: %d seconds.", Config.Attack, seconds)
	duration := time.Duration(seconds*1000) * time.Millisecond
	server := strings.TrimSuffix(Config.Server, "/")
	for idx, attacker := range attackProfile.Attackers {
		if len(attacker.Endpoints) == 0 {
			log.Fatalf("Empty endpoint list encountered for %s", Config.Attack)
		}

		frequency := attacker.Frequency
		if Config.Frequency > 0 {
			frequency = Config.Frequency
		}
		rate := vegeta.Rate{
			Freq: frequency,
			Per:  time.Second,
		}

		var targets []vegeta.Target
		for _, endpoint := range attacker.Endpoints {
			targets = append(targets, vegeta.Target{
				Method: "GET",
				URL:    fmt.Sprintf("%s/%s", server, strings.TrimLeft(endpoint, "/")),
			})
		}

		targeter := vegeta.NewStaticTargeter(targets...)
		// Truncate all response bodies at 100 bytes
		vegAttacker := vegeta.NewAttacker(vegeta.MaxBody(100))

		wg.Add(1)
		go func(attacker *Attacker) {
			defer wg.Done()
			remainder := duration
			if attacker.Delay > 0 {
				if attacker.Delay >= float32(attackProfile.Duration) {
					log.Printf("The specified delay, %f, for attacker %d is >= the duration of test, %d.", attacker.Delay, idx, Config.Attack)
					return
				}
				attackDelay := time.Duration(int(attacker.Delay*1000)) * time.Millisecond
				time.Sleep(attackDelay)
				remainder -= attackDelay
			}
			if attacker.Name != "" {
				log.Printf("Beggining %s", attacker.Name)
			}
			for res := range vegAttacker.Attack(targeter, rate, remainder, strconv.Itoa(idx)) {
				resChan <- res
			}
		}(attacker)
	}

	// Make a notification channel for the end of the attack.
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()

	// A couple of helper functions.
	// Vegeta doesn't return the url in the result, so look it up,
	// finding the Attacker along the way.
	findAttacker := func(result *vegeta.Result) (attacker *Attacker, endpoint string) {
		attackerIdx, err := strconv.Atoi(result.Attack)
		if err != nil {
			log.Fatalf("Failed to parse attacker index. Something is terribly wrong.")
			return
		}
		attacker = attackProfile.Attackers[attackerIdx]
		endpoint = attacker.Endpoints[int(result.Seq)%len(attacker.Endpoints)]
		return
	}

	// A list of unique error URLs. Print messages for new ones. Ignore repeats.
	var problematic []string
	addProblematic := func(result *vegeta.Result) {
		_, endpoint := findAttacker(result)
		for _, url := range problematic {
			if url == endpoint {
				return
			}
		}
		problematic = append(problematic, endpoint)
		if result.Code == 0 {
			log.Printf("Code 0 requesting %s. Connection failed.", endpoint)
		} else {
			log.Printf("Error code %d from %s: %s",
				result.Code, endpoint, string(result.Body))
		}
	}

	delay := time.Duration(1) * time.Millisecond
	var metrics vegeta.Metrics
	var res *vegeta.Result

	// Monitor results. Wait for end-of-attack signal.
out:
	for {
		select {
		case res = <-resChan:
			if res.Code != 200 {
				addProblematic(res)
			}
			// If more info is needed for e.g. plotting, grab it here.
			metrics.Add(res)
		case <-c:
			break out
		case <-time.After(delay):
		}
	}
	metrics.Close()

	// Vegeta report to terminal.
	vegeta.NewTextReporter(&metrics)(os.Stdout)

	// JSON results file.
	fmtTime := time.Now().Format("2016-01-02-15-04-05")
	var jsonBytes []byte
	if Config.FormatResults {
		jsonBytes, err = json.MarshalIndent(metrics, "", "    ")
	} else {
		jsonBytes, err = json.Marshal(metrics)
	}
	if err != nil {
		log.Fatalf("Failed to parse JSON from vegeta.Metrics for %s:", Config.Attack)
	}
	path := filepath.Join(Config.ResultDirectory, fmt.Sprintf(outputFileTemplate, Config.Attack, fmtTime))
	err = ioutil.WriteFile(path, jsonBytes, 0755)
	if err != nil {
		log.Fatalf("Failed to write result file to %s", path)
	}

	return
}

func createResultsDirectory() (err error) {
	fileInfo, err := os.Stat(Config.ResultDirectory)
	if os.IsNotExist(err) {
		err = os.MkdirAll(Config.ResultDirectory, 0755)
		if err != nil {
			return fmt.Errorf("Unable to create results directory: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("Unable to retrieve file system information: %v", err)
	} else if !fileInfo.IsDir() {
		return fmt.Errorf("Unable to create results directory due to a filename conflict.")
	}
	return
}

func loadProfiles() (profiles map[string]*Attack, err error) {
	jsonBytes, err := ioutil.ReadFile(Config.ProfilesPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to load attack profile definitions from %s: %v", Config.ProfilesPath, err)
	}

	if err = json.Unmarshal(jsonBytes, &profiles); err != nil {
		return nil, fmt.Errorf("Failed to parse JSON from attack profiles: %v", err)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("No attack profiles found.")
	}

	return
}

func listAttacks(profiles map[string]*Attack) {
	fmt.Println("available attacks")
	fmt.Println("-----------------")
	for key := range profiles {
		fmt.Println(key)
	}
}
