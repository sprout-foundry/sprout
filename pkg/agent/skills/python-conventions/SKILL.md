---
name: python-conventions
description: Python 3.11+ key decisions and patterns. Use when writing Python code.
---

# Python Conventions

## Key Decisions

- **Type hints**: Use modern `|` syntax (Python 3.10+): `str | None` instead of `Optional[str]`
- **Data classes**: Prefer `@dataclass` for structured data over manual `__init__`
- **Error handling**: Use `Result` pattern or explicit exceptions, never bare `except:`
- **Project structure**: Use `src/` layout with `pyproject.toml`

## Naming

| Type | Convention | Example |
|------|-----------|---------|
| Variables/Functions | `snake_case` | `user_name`, `get_users` |
| Classes | `PascalCase` | `UserService` |
| Constants | `UPPER_SNAKE_CASE` | `MAX_RETRIES` |

## Common Pitfalls

```python
# BAD: Mutable default - shared across calls!
def add_item(item, items=[]):

# GOOD: Use None
def add_item(item, items=None):
    if items is None:
        items = []

# BAD: Late binding closure
[lambdas = [lambda x: i * x for i in range(5)]  # All use i=4

# GOOD: Capture value
lambdas = [lambda x, i=i: i * x for i in range(5)]
```

## Tools

- **Format**: `black` or `ruff format`
- **Lint**: `ruff` (replaces flake8, isort)
- **Type check**: `mypy --strict`
- **Test**: `pytest`

## Testing Pattern

```python
# test_user.py
@pytest.fixture
def user():
    return User(name="test")

def test_user_creation(user):
    assert user.name == "test"
```
