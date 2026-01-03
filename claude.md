# Claude Code Rules

## Git Commit Rules

All commits MUST:
1. Include the `--signoff` flag (`-s`)
2. Follow conventional commit format: `type(scope): message`

### Commit Types
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation changes
- `style` - Code style/formatting (no logic change)
- `refactor` - Code refactoring
- `perf` - Performance improvement
- `test` - Adding/updating tests
- `chore` - Maintenance tasks, dependencies

### Format
```
type(scope): short description

Optional longer description if needed.
```

### Examples
```bash
git commit -s -m "feat(workflow): add template execution engine"
git commit -s -m "fix(k8s): resolve pod status sync issue"
git commit -s -m "chore(deps): update go modules"
```

Do NOT add any Claude attribution, co-author lines, or "Generated with Claude" messages.

## Repository Info

- Author Email: mohitnagaraj20@gmail.com
