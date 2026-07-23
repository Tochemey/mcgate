# 🤝 Contributing to portcullis

Thank you for your interest in contributing to portcullis! Every contribution — whether it is a bug report, feature suggestion, documentation improvement, or code change — is valued and appreciated.

Before you begin, please read the project [README](README.md) to understand the architecture, core concepts, and public API surface.

## 📋 Table of Contents

- [Code of Conduct](#-code-of-conduct)
- [Getting Started](#-getting-started)
- [Development Workflow](#-development-workflow)
- [Code Standards](#-code-standards)
- [Testing](#-testing)
- [Commit Messages](#-commit-messages)
- [Pull Requests](#-pull-requests)
- [Reporting Issues](#-reporting-issues)
- [License](#-license)

## 🌍 Code of Conduct

This project follows a standard of mutual respect and constructive collaboration. Be kind, be patient, and assume good intent. Harassment, discrimination, and disruptive behaviour will not be tolerated.

## 🚀 Getting Started

### Prerequisites

| Requirement | Minimum Version |
|-------------|-----------------|
| Go          | 1.26+           |
| Git         | 2.x             |

For working on the cluster example, you will also need:

| Requirement | Purpose                    |
|-------------|----------------------------|
| Docker      | Container runtime for Kind |
| Kind        | Local Kubernetes cluster   |
| kubectl     | Kubernetes CLI             |

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/<your-username>/portcullis.git
   cd portcullis
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/Tochemey/portcullis.git
   ```

### Verify Your Setup

```bash
go test ./...
```

All tests should pass before you make any changes.

## 🔄 Development Workflow

1. **Sync with upstream** — always start from a fresh `main`:
   ```bash
   git fetch upstream
   git checkout main
   git merge upstream/main
   ```

2. **Create a feature branch** — use a descriptive name:
   ```bash
   git checkout -b feat/my-feature
   ```

3. **Make your changes** — keep commits focused and incremental

4. **Run checks locally** before pushing (see [Testing](#-testing) and [Code Standards](#-code-standards))

5. **Push and open a pull request** against `main`

## ✅ Code Standards

### Linting

The project uses [golangci-lint](https://golangci-lint.run/) v2 with the configuration in `.golangci.yml`. The enabled linters include:

| Linter        | Purpose                            |
|---------------|------------------------------------|
| `gocyclo`     | Cyclomatic complexity              |
| `gosec`       | Security issue detection           |
| `misspell`    | Spelling mistakes in comments/docs |
| `revive`      | Opinionated Go style checks        |
| `staticcheck` | Advanced static analysis           |
| `whitespace`  | Unnecessary blank lines            |
| `govet`       | Go vet correctness checks          |
| `goheader`    | License header enforcement         |

Run the linter locally:

```bash
golangci-lint run ./...
```

### Formatting

Code formatting is enforced by `gofmt` and `goimports` via golangci-lint. Run:

```bash
goimports -w .
```

### License Header

Every `.go` file must include the MIT license header. The `goheader` linter enforces this automatically. If you create a new file, add the following header at the top:

```
// MIT License
//
// Copyright (c) 2026 GoAkt Team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// ...
```

The full header template is defined in `.golangci.yml`.

### Vendor Directory

This project vendors its dependencies. After adding or updating a dependency:

```bash
go mod tidy
go mod vendor
```

Always commit vendor changes in the same pull request as the code that requires them.

## 🧪 Testing

### Running Tests

```bash
# All tests
go test ./...

# With race detection
go test -race ./...

# Specific package
go test ./mcp/...

# Verbose output for a specific test
go test -v -run TestMyFunction ./path/to/package/...
```

### Writing Tests

- Place test files alongside the code they test (`foo_test.go` next to `foo.go`)
- Use table-driven tests where multiple input/output scenarios exist
- Use `testify/assert` and `testify/require` for assertions — both are already dependencies
- Test public behaviour, not internal implementation details
- Avoid mocks when a real in-memory implementation is available (e.g. `MemorySink` for audit tests)

### Test Coverage

Aim for meaningful coverage of new code. Do not add tests purely to increase coverage numbers — focus on tests that verify correctness and catch regressions.

## 💬 Commit Messages

Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>: <short description>
```

| Type       | When to use                                             |
|------------|---------------------------------------------------------|
| `feat`     | A new feature                                           |
| `fix`      | A bug fix                                               |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `test`     | Adding or updating tests                                |
| `docs`     | Documentation changes                                   |
| `chore`    | Build process, CI, dependency updates                   |

Keep the subject line under 72 characters. Use the body for context when the change is non-obvious.

## 🔀 Pull Requests

### Before Submitting

- [ ] All tests pass locally (`go test ./...`)
- [ ] Linter passes (`golangci-lint run ./...`)
- [ ] Code is formatted (`goimports -w .`)
- [ ] Vendor directory is up to date (if dependencies changed)
- [ ] New public API has appropriate documentation
- [ ] Commit messages follow the conventional commits format

### PR Guidelines

- **One concern per PR.** A bug fix and a new feature should be separate pull requests.
- **Keep PRs small.** Smaller changes are easier to review and merge. If a feature is large, consider splitting it into incremental PRs.
- **Describe the why.** The PR description should explain the motivation for the change, not just what changed. Link to any relevant issue.
- **No breaking changes without discussion.** If your change modifies the public API (the `Gateway` methods or the `mcp` package types), open an issue first to discuss the design.

### Review Process

- All pull requests require at least one approval before merging
- Maintainers may request changes — this is normal and part of the collaborative process
- Once approved, a maintainer will merge using squash-and-merge to keep the history clean

## 🐛 Reporting Issues

### Bug Reports

When filing a bug report, please include:

- **Go version** (`go version`)
- **portcullis version** (commit hash or release tag)
- **Steps to reproduce** — minimal, self-contained example preferred
- **Expected behaviour** vs. **actual behaviour**
- **Relevant logs or error messages**

### Feature Requests

Feature requests are welcome. Please describe:

- **The problem** you are trying to solve
- **Your proposed solution** (if you have one)
- **Alternatives you considered**

Open an issue with a clear title and the appropriate label.

## 📜 License

By contributing to portcullis, you agree that your contributions will be licensed under the [MIT License](LICENSE).
