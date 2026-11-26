| workflow | runner | n | queue median (p90) | first step median (p90) | total median (p90) | total min-max | failed |
|---|---|---|---|---|---|---|---|
| go-build | hosted | 10 | 3s (4s) | 5s (5s) | 47s (55s) | 40s-57s | 0 |
| go-build | mayfly | 10 | 2s (2s) | 5s (5s) | 83s (85s) | 79s-86s | 0 |
| npm-test | hosted | 10 | 3s (3s) | 5s (7s) | 13s (24s) | 11s-26s | 0 |
| npm-test | mayfly | 10 | 2s (2s) | 5s (6s) | 28s (29s) | 27s-30s | 0 |
