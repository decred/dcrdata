# dcrdata

This is very much a work-in-progress!

What it is/does presently:

* Registers for new-block notifications (and other ntfns) from dcrd
* When signaled by the notifier, collects a bunch of block data
* Saves it into a map in memory
* Starts a HTTP JSON API and serves up what it has

What it needs (just a few top ones):

* A real database backend, perhaps PostgreSQL and/or mongodb
* A command line utility, in cmd/rebuilddb for instance, to perform a full block
  chain scan to get past data into the database.
* mempool data collection and storage. Collection is already implemented, but no
storage or API endpoints.  For instance, a realtime read of the ticket fee
distribution in mempool, stake submissions included as they come in.
* An actual web interface.  However, with the JSON API, perhaps a separate
process or a plain old php+js page from a web server could consume and present
the data. Maybe there's no need for php here, given golang templates.
* Refactored into reusable golang packages.  Secondary apps in a cmd directory.
