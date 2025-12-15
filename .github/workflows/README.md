# GitHub Actions Workflows

This directory contains reusable workflows for Go projects.

## Features

- ✅ **Test Results as Artifacts**: Downloadable from workflow run summary
- ✅ **PR Comments**: Test summary posted directly in PR comments
- ✅ **Check Annotations**: Failed tests appear as annotations in the PR checks
- ✅ **Path Filtering**: Only runs when Go files change
- ✅ **Skip CI Support**: Respects `[skip-ci]` flag
- ✅ **Private Module Support**: Configured for private Go modules

## Test Results in Pull Requests

### Artifacts vs Comments

**GitHub Actions Limitations:**
- Unlike GitLab, GitHub Actions **cannot** display artifacts directly inline in PRs
- Artifacts are stored separately and must be downloaded from the workflow run summary
- The standard approach for showing test results in GitHub PRs is through **PR comments** and **check annotations**

**Our Solution:**
We use a **dual approach**:
1. **Artifacts**: Test results are uploaded as artifacts for downloadability and archival
2. **PR Comments**: Test results are posted as comments on PRs using `dorny/test-reporter` action

### How to Use in Other Repositories

1. **Copy the workflow** to your repository:
   ```bash
   cp .github/workflows/test.yml /path/to/repo/.github/workflows/test.yml
   ```

2. **Customize as needed**:
   - Adjust `GOPRIVATE` environment variable if needed
   - Modify test command if you need coverage or specific test flags
   - Add/remove path filters in the `on:` section
   - Adjust timeout values

3. **Ensure permissions** are set correctly:
   ```yaml
   permissions:
     contents: read
     checks: write
     pull-requests: write
   ```

4. **For private modules**, ensure `GH_ACCESS_TOKEN` secret is configured in your repository settings.

### Features

- ✅ **Test Results as Artifacts**: Downloadable from workflow run summary
- ✅ **PR Comments**: Test summary posted directly in PR comments
- ✅ **Check Annotations**: Failed tests appear as annotations in the PR checks
- ✅ **Generic & Reusable**: Works across all Go repositories
- ✅ **Path Filtering**: Only runs when relevant files change
- ✅ **Skip CI Support**: Respects `[skip-ci]` flag

### Example Output

When tests run, you'll see:
- **In PR Comments**: A formatted test summary with pass/fail counts
- **In PR Checks**: Individual test failures as annotations
- **In Artifacts**: Downloadable `test-results.xml` file

### Customization Examples

**Add coverage:**
```yaml
- name: Run tests with coverage
  run: |
    gotestsum --junitfile test-results.xml --format standard-verbose -coverprofile=coverage.out ./...
```

**Test specific packages:**
```yaml
- name: Run tests
  run: |
    gotestsum --junitfile test-results.xml --format standard-verbose ./pkg/... ./cmd/...
```

**Add custom paths:**
```yaml
on:
  push:
    paths:
      - '**/*.go'
      - 'go.mod'
      - 'go.sum'
      - '.github/**'  # Add this to trigger on workflow changes
```

