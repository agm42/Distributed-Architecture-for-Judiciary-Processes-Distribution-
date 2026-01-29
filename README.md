# Distributed Architecture for Lawsuits Distribution
A distributed approach for lawsuits distribution to be used in the Court of Justice of São Paulo, Brazil.

There is 3 agents: court, district and trial

Compile:
```
$ go build court.go
$ go build distric.go
$ go build trial.go
```


For testing using local host:

**1)** Run court in its folder (will be used the default UDP address for court: 127.0.0.1:9000):
```
$ ./court
```


**2)** Add 2 districts (e.g.: Campinas and Taubate) using the option "A" form court's main menu:

```
District's name: Campinas
District's UDP address: 127.0.0.1:9100
Number of trials: 3
```

```
District's name: Taubate
District's UDP address: 127.0.0.1:9200
Number of trials: 3
```


**3)** Run each district in its own folder (different than the folder where court, or other districts/trials, is running, due the configuration files automatically create).
```
$ ./district -name Campinas
$ ./district -name Taubate
```


**4)** Add trials for the districts using the option "A" from each district's main menu:

```
    UDP address for the new trial (ex: 127.0.0.1:9201): 127.0.0.1:9101
    UDP address for the new trial (ex: 127.0.0.1:9201): 127.0.0.1:9102
    UDP address for the new trial (ex: 127.0.0.1:9201): 127.0.0.1:9103
```

```
    UDP address for the new trial (ex: 127.0.0.1:9201): 127.0.0.1:9201
    UDP address for the new trial (ex: 127.0.0.1:9201): 127.0.0.1:9202
    UDP address for the new trial (ex: 127.0.0.1:9201): 127.0.0.1:9203
```


**5)** Run each trial in its own folder different than the folders where court/districts and other trials are running (due the configuration files automatically created).

```
$ ./trial -district 127.0.0.1:9100 -id 1
$ ./trial -district 127.0.0.1:9100 -id 2
$ ./trial -district 127.0.0.1:9100 -id 3
```

```
$ ./trial -district 127.0.0.1:9200 -id 1
$ ./trial -district 127.0.0.1:9200 -id 2
$ ./trial -district 127.0.0.1:9200 -id 3
```


**6)** Manage the lawsuits using the districts and trials menu.
