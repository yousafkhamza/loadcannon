# loadcannon
Load-test internal and public HTTP APIs from one scenario format. Wraps [k6](https://k6.io) for the actual traffic generation; loadcannon handles config, target resolution (LB / direct IP / hostname), and secure secret injection. Single static binary, zero third-party Go dependencies.
