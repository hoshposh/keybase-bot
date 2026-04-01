# Contributing to Keybase-Obsidian Bot

First off, thanks for taking the time to contribute!

## Getting Started

1. Fork the repository and create your branch from `main`.
2. Ensure you have Go installed (refer to `go.mod` for the required version).
3. If you haven't already, install [Task](https://taskfile.dev/installation/).

## Development & Testing

- To build the binary locally, run: `task build`
- To run the unit tests, run: `task test`

### Code Style

- Please ensure your code is formatted with `gofmt` before committing.
- Keep tests green: all new features should have accompanying unit tests.

## Pull Request Process

1. Ensure the CI checks pass on your PR.
2. Provide a clear description of the problem and the solution in your PR.
3. Once approved by a maintainer, your PR will be squash-merged into `main`.
