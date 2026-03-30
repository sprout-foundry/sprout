# Contributing to Ledit

We welcome contributions to Ledit! Your help is invaluable in making this project better.

## Repository

The official repository for Ledit is:
**https://github.com/alantheprice/ledit**

Please fork this repository to submit your contributions.

## How to Contribute

### Reporting Bugs

If you find a bug, please open an issue on the [issue tracker](https://github.com/alantheprice/ledit/issues).
When reporting a bug, please include:
- A clear and concise description of the bug
- Steps to reproduce the behavior
- Expected behavior
- Screenshots or error messages if applicable
- Your operating system and ledit version (`ledit version`)

### Suggesting Enhancements

If you have a suggestion for a new feature, please open an issue on the [issue tracker](https://github.com/alantheprice/ledit/issues).
When suggesting an enhancement, please include:
- A clear and concise description of the proposed feature
- Why you think it would be useful
- Any potential use cases

### Submitting Pull Requests

1. **Fork the repository** from [github.com/alantheprice/ledit](https://github.com/alantheprice/ledit)
2. **Clone your fork:** `git clone https://github.com/YOUR_USERNAME/ledit.git`
3. **Create a branch:** `git checkout -b feature/your-feature-name` or `git checkout -b bugfix/your-bug-fix`
4. **Make your changes.** Run `make test-unit` and `go build ./...` before committing.
5. **Commit:** Use conventional commits: `git commit -m "feat: add new feature X"` or `git commit -m "fix: resolve bug Y"`
6. **Push:** `git push origin feature/your-feature-name`
7. **Open a Pull Request** against the `main` branch.

## Code Style

Please adhere to the existing code style. We use `gofmt` and `goimports` for Go code. See [docs/TESTING.md](docs/TESTING.md) for test guidelines.

## License

By contributing to Ledit, you agree that your contributions will be licensed under the project's [LICENSE](LICENSE) file.