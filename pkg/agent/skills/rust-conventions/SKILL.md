---
name: rust-conventions
description: Rust 2021 edition coding conventions. Use when writing or reviewing Rust code.
---

# Rust Conventions

Key decisions and patterns for Rust 2021.

## Naming

| Type | Convention | Example |
|------|-----------|---------|
| Structs/Enums/Traits | `PascalCase` | `UserService`, `Result` |
| Functions/Variables | `snake_case` | `get_users`, `user_count` |
| Constants | `SCREAMING_SNAKE_CASE` | `MAX_RETRIES` |
| Modules | `snake_case` | `user_service` |

## Key Decisions

```rust
// Getter naming: no get_ prefix
pub fn first(&self) -> &First { &self.first }
pub fn first_mut(&mut self) -> &mut First { &mut self.first }

// Use get_ only for collections
fn get(&self, key: K) -> Option<&V>;

// Conversion prefixes
// as_ → free, borrowed → borrowed
// to_ → expensive, borrowed → owned  
// into_ → consumes self, owned → owned
```

## Error Handling

```rust
// Libraries: use thiserror
#[derive(Error, Debug)]
pub enum DataStoreError {
    #[error("data store disconnected")]
    Disconnect(#[from] io::Error),
}

// Applications: use anyhow
fn read_config(path: &Path) -> Result<Config> {
    let contents = fs::read_to_string(path)
        .context("failed to read config")?;
    Ok(parse(&contents)?)
}

// Never use () as error type
```

## Common Pitfalls

```rust
// BAD: Mutable borrow conflict
let mut v = vec![1, 2, 3];
let first = &v[0];
v.push(4);  // Error!

// GOOD: Minimize borrow scope
let mut v = vec![1, 2, 3];
let first = v[0];  // Copy, not borrow
v.push(4);

// BAD: Missing lifetime
fn longest(x: &str, y: &str) -> &str

// GOOD: Explicit lifetime
fn longest<'a>(x: &'a str, y: &'a str) -> &'a str
```

## Tools

- **Format**: `cargo fmt`
- **Lint**: `cargo clippy -- -D warnings`
- **Test**: `cargo test`

## Testing Pattern

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_valid() {
        let result = parse("test").unwrap();
        assert_eq!(result.value, 42);
    }

    #[test]
    fn test_parse_file() -> Result<()> {
        let content = fs::read_to_string("test.txt")?;
        assert!(parse(&content).is_ok());
        Ok(())
    }
}
```
