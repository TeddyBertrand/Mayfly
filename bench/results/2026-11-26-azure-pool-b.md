| workflow | runner | n | queue median (p90) | first step median (p90) | total median (p90) | total min-max | failed |
|---|---|---|---|---|---|---|---|
| go-build | hosted | 10 | 3s (3s) | 4.5s (6s) | 47.5s (55s) | 40s-59s | 0 |
| go-build | mayfly | 10 | 2s (2s) | 5s (5s) | 84.5s (86s) | 80s-87s | 0 |
| npm-test | hosted | 10 | 3s (4s) | 5s (7s) | 14s (23s) | 10s-23s | 0 |
| npm-test | mayfly | 10 | 2s (2s) | 5s (6s) | 29s (30s) | 27s-30s | 0 |
