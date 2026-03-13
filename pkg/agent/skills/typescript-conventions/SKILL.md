---
name: typescript-conventions
description: TypeScript 5.x / JavaScript ES2022+ coding conventions. Use when writing or reviewing TypeScript/JavaScript code.
---

# TypeScript Conventions

Key decisions and patterns for TypeScript 5.x / ES2022+.

## Naming

| Type | Convention | Example |
|------|-----------|---------|
| Classes/Interfaces/Types | `PascalCase` | `UserService`, `User` |
| Variables/Functions | `camelCase` | `userName`, `getUsers` |
| Constants | `SCREAMING_SNAKE_CASE` | `MAX_RETRIES` |
| Files | `camelCase.ts` | `userService.ts` |
| React Components | `PascalCase.tsx` | `UserProfile.tsx` |

## Key Decisions

```typescript
// Prefer interface for object shapes
interface User {
  id: string;
  name: string;
}

// Use type for unions/intersections
type Status = 'active' | 'inactive';

// Avoid any - use unknown
function parse(data: unknown): User {
  if (isUser(data)) return data;
  throw new Error('Invalid');
}

// Type guard over assertion
function isUser(obj: unknown): obj is User {
  return typeof obj === 'object' && obj !== null && 'id' in obj;
}
```

## Modern Features

```typescript
// Optional chaining & nullish coalescing
const name = user?.profile?.name ?? 'Anonymous';

// Use ?? not || for defaults (0 and '' are valid)
const count = config.count ?? 10;  // Not ||

// Prefer async/await
async function fetchUser(id: string): Promise<User> {
  const res = await fetch(`/users/${id}`);
  if (!res.ok) throw new Error('Not found');
  return res.json();
}
```

## Common Pitfalls

```typescript
// BAD: any type
function process(data: any) { }

// GOOD: unknown with type guard
function process(data: unknown) {
  if (isUser(data)) { /* safe */ }
}

// BAD: Type assertion without validation
const user = data as User;

// GOOD: Runtime validation
if (isUser(data)) { /* safe */ }
```

## Tools

- **Format**: Prettier
- **Lint**: ESLint + typescript-eslint
- **Test**: Vitest (new) or Jest

## Testing Pattern

```typescript
describe('UserService', () => {
  it('should return user when found', async () => {
    const service = new UserService(mockRepo);
    const result = await service.findById('1');
    expect(result).toEqual({ id: '1', name: 'Test' });
  });
});
```
