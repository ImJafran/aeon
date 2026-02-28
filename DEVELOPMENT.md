# Development

Aeon is fully open source and contributions are welcome from everyone. For architecture and internals, see the [Engineering Deep Dive](ENGINEERING.md). For getting started, see the [README](README.md).

---

## Getting Started

```bash
# Fork on GitHub, then:
git clone https://github.com/YOUR_USERNAME/aeon.git
cd aeon
make build && make test
```

---

## Branch Naming

Create a branch using the format `yourname/feature-or-bug-name`:

```bash
git checkout -b yourname/add-discord-channel
git checkout -b yourname/fix-memory-search
```

---

## Build & Test

```bash
make build          # CGO_ENABLED=0, builds ./bin/aeon
make test           # run all tests
make test-verbose   # verbose test output
make lint           # run golangci-lint
make build-linux    # cross-compile for linux/amd64 and linux/arm64
make install        # build + install to ~/.local/bin/aeon
make clean          # remove build artifacts
```

### Docker (for development)

```bash
docker compose run --rm test           # run tests
docker compose run --rm dev            # interactive CLI in container
docker compose up serve -d             # start all channels
docker compose down                    # stop
```

---

## Submitting a Pull Request

1. **Fork** the repository
2. **Create a branch** with the naming format above
3. **Make your changes** — keep commits focused and atomic
4. **Run tests** — `make test` must pass
5. **Push** to your fork
6. **Open a Pull Request** with:
   - A clear title describing the change
   - What you changed and why
   - How to test it

For larger changes, please open an issue first to discuss the approach.

---

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep it simple — no unnecessary abstractions
- Tests for new functionality
- No external LLM SDK dependencies — providers call raw HTTP APIs

---

## Author

Created by **[Jafran Hasan](https://linkedin.com/in/iamjafran)** ([@imjafran](https://github.com/ImJafran))

## License

[MIT](LICENSE)
