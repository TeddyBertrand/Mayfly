| workflow | runner | n | queue median (p90) | first step median (p90) | total median (p90) | total min-max | failed |
|---|---|---|---|---|---|---|---|
| go-build | hosted | 10 | 2.5s (7s) | 4.5s (10s) | 48s (59s) | 36s-61s | 0 |
| go-build | mayfly | 10 | 6s (7s) | 9s (10s) | 49s (49s) | 49s-50s | 0 |
| npm-test | hosted | 10 | 3s (5s) | 5s (8s) | 13s (15s) | 10s-25s | 0 |
| npm-test | mayfly | 10 | 6s (7s) | 9s (10s) | 17s (18s) | 17s-18s | 0 |
