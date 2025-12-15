# Quick Setup Guide

## Copy Test Workflow to Other Repositories

Run this command from the `magic` repository root:

```bash
# For base repository
cp .github/workflows/test.yml ../source/base/.github/workflows/test.yml

# For ledge repository
cp .github/workflows/test.yml ../source/ledge/.github/workflows/test.yml

# For flow repository
cp .github/workflows/test.yml ../source/flow/.github/workflows/test.yml

# For wire repository
cp .github/workflows/test.yml ../source/wire/.github/workflows/test.yml

# For imagine repository
cp .github/workflows/test.yml ../source/imagine/.github/workflows/test.yml

# For matchblox repository
cp .github/workflows/test.yml ../source/matchblox/.github/workflows/test.yml

# For profile repository
cp .github/workflows/test.yml ../source/profile/.github/workflows/test.yml
```

## After Copying

1. **Review the workflow file** - Ensure paths and configurations match your repository
2. **Check permissions** - Verify the workflow has `pull-requests: write` permission
3. **Test it** - Create a test PR to verify test results appear correctly
4. **Remove old test steps** - If updating existing workflows, remove duplicate test steps

## What You Get

- ✅ Test results posted as PR comments
- ✅ Test results uploaded as downloadable artifacts
- ✅ Failed tests shown as check annotations
- ✅ Only runs when Go files change
- ✅ Respects `[skip-ci]` flag

