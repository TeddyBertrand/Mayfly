| workflow | runner | n | queue median (p90) | first step median (p90) | total median (p90) | total min-max | failed |
|---|---|---|---|---|---|---|---|
| go-build | hosted | 10 | 3s (3s) | 5s (6s) | 47s (52s) | 38s-52s | 0 |
| go-build | mayfly | 10 | 7s (7s) | 10s (10s) | 49s (50s) | 48s-50s | 0 |
| npm-test | hosted | 10 | 3s (4s) | 5s (6s) | 13s (19s) | 11s-20s | 0 |
| npm-test | mayfly | 10 | 7s (7s) | 9.5s (10s) | 17.5s (18s) | 17s-24s | 0 |
