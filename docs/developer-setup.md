# Developer Setup

This project uses pre-commit hooks to enforce Go formatting, import grouping, and linting.
To enable these checks locally, first install the [pre-commit](https://pre-commit.com/) framework:

```bash
pip install pre-commit
```

Then install the Git hook scripts:

```bash
pre-commit install
```

To run all checks against all files (e.g. after first install):

```bash
pre-commit run --all-files
```

The configured hooks include:
- `gofmt` (with simplification via `-s`)
- `goimports` (organizes and groups imports according to the module path)
- `golangci-lint` (runs govet, staticcheck, errcheck, ineffassign, unused, and more)

These checks are also executed in CI to ensure consistent code quality.
