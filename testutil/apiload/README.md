# API Load Testing

This utility performs a load test on the specified server and endpoints.
The test (a.k.a. attack) duration, frequency, and endpoints are specified in a
JSON formatted attack definition file. Check out the default definitions in
`profiles.json` to see the format.

### Options
```
-c, --config=    Path to a custom configuration file.
-d, --directory= Directory for the result files. (results)
-p, --profiles=  Path to custom attack profiles. (profiles.json)
-s, --server=    Target base URL, with protocol. (http://localhost:7777)
-a, --attack=    The attack to perform. The attack must match an entry in the definitions file.
-l, --list       List available attacks from attack profiles.
-f, --format     Pretty print the JSON result file.
    --cpus=      Maximum number of processors to use. Defaults to all available.
    --duration=  Overrides the duration of the chosen attack. Units are seconds.
    --frequency= Overrides the attack frequency for all attackers. Units are requests/second.
-h, --help       Show this help message
```

All options (except `--config`) can be set via either the CLI or an INI
configuration file. `--attack` is the only required argument.

Attack definitions (from `--profiles`) may have multiple attackers running
simultaneously, each with their own frequency. If a `--frequency` is provided,
the value will override the frequencies specified in the definitions for every
attacker. For finer control, create a custom attack definition.

### Example output
```
2019/01/08 06:51:08 Error code 422 from /api/tx/b7f3dd99ba3bbefead4afd519007cd3c49cc2539a2d4786f2f7f78a33bf1e4ae/trimmed: Unprocessable Entity
Requests      [total, rate]            300, 5.02
Duration      [total, attack, wait]    1m0.031037037s, 59.800107541s, 230.929496ms
Latencies     [mean, 50, 95, 99, max]  304.206028ms, 23.011614ms, 578.588264ms, 7.553415001s, 7.798093224s
Bytes In      [total, mean]            25806, 86.02
Bytes Out     [total, mean]            0, 0.00
Success       [ratio]                  98.67%
Status Codes  [code:count]             200:296  422:4  
Error Set:
422 Unprocessable Entity
```
