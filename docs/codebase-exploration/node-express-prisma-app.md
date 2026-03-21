# Codebase Exploration: node-express-prisma-v1

**Repo path:** `~/Documents/projects/ai_engineering/gauntlet-curriculum/projects/node-express-prisma-v1-official-app/`

**Explored:** 2026-03-21 — manual file-by-file inspection, no inference from training data.

This document is the acceptance criteria for the Phase 1 indexer. If automated extraction does not match what is documented here, the indexer is wrong.

---

## Tech Stack (detected from package.json / tsconfig.json / prisma/schema.prisma)

| Component | Technology |
|---|---|
| Runtime | Node.js (target: ES5 transpiled via tsc) |
| Framework | Express 4.17.1 |
| ORM | Prisma 2.29.1 (Client: @prisma/client) |
| Language | TypeScript 4.4.2 (strict mode) |
| Test framework | Jest 27.1.0 + ts-jest + jest-mock-extended |
| Linter | ESLint 7.32.0 (airbnb-base + prettier + @typescript-eslint/parser) |
| Formatter | Prettier 2.4.0 (via eslint-config-prettier) |
| Auth | express-jwt 6.1.0 + jsonwebtoken 8.5.1 (JWT HS256) |
| Password | bcryptjs 2.4.3 |
| Pre-commit | Husky + lint-staged |
| API docs | swagger-ui-express |

---

## Directory Layout

```
root/
├── prisma/
│   ├── schema.prisma               # Prisma schema (models, datasource, generator)
│   ├── prisma-client.ts            # Singleton PrismaClient instance
│   └── migrations/
│       ├── 20210924222830_initial/ # Each migration is a timestamped directory
│       │   └── migration.sql
│       ├── 20211001195651_implicit_articles/
│       │   └── migration.sql
│       └── 20211105082430_api_url/
│           └── migration.sql
├── src/
│   ├── index.ts                    # Express app setup, middleware, error handler, listen
│   ├── routes/
│   │   └── routes.ts               # Route composition — mounts all controllers at /api
│   ├── controllers/                # Route handlers (one file per entity)
│   │   ├── article.controller.ts
│   │   ├── auth.controller.ts
│   │   ├── profile.controller.ts
│   │   └── tag.controller.ts
│   ├── services/                   # Business logic + Prisma queries (one file per entity)
│   │   ├── article.service.ts
│   │   ├── auth.service.ts
│   │   ├── profile.service.ts
│   │   └── tag.service.ts
│   ├── models/                     # TypeScript interfaces/types
│   │   ├── article.model.ts
│   │   ├── comment.model.ts
│   │   ├── http-exception.model.ts
│   │   ├── profile.model.ts
│   │   ├── registered-user.model.ts
│   │   ├── register-input.model.ts
│   │   ├── tag.model.ts
│   │   └── user.model.ts
│   └── utils/
│       ├── auth.ts                 # express-jwt middleware config
│       ├── profile.utils.ts        # Profile response mapper
│       ├── token.utils.ts          # JWT token generation/verification
│       └── user-request.d.ts       # Type augmentation for Express Request
├── tests/
│   ├── prisma-mock.ts              # Global Prisma mock setup (jest-mock-extended)
│   ├── services/
│   │   ├── article.service.test.ts
│   │   ├── auth.service.test.ts
│   │   ├── profile.service.test.ts
│   │   └── tag.service.test.ts
│   └── utils/
│       └── profile.utils.test.ts
├── docs/
│   └── swagger.json
├── public/                         # Static file serving
├── package.json
├── tsconfig.json
├── jest.config.js
└── .eslintrc.json
```

---

## 1. Migrations

### Location
`prisma/migrations/` — each migration is a directory named `{timestamp}_{description}/migration.sql`.

### Naming Convention
`{YYYYMMDDHHmmss}_{snake_case_description}/migration.sql`

Examples:
- `20210924222830_initial/migration.sql`
- `20211001195651_implicit_articles/migration.sql`
- `20211105082430_api_url/migration.sql`

### How Migrations Are Generated
Prisma generates migrations via `prisma migrate dev`. The developer modifies `prisma/schema.prisma`, then runs the command. Prisma computes the diff and writes the SQL. Migrations are **never written by hand** in this codebase — they are generated artifacts.

The `migration_lock.toml` file pins the Prisma engine version to prevent drift.

### Typical Migration Structure

```sql
-- CreateTable
CREATE TABLE "Article" (
    "id" SERIAL NOT NULL,
    "slug" TEXT NOT NULL,
    "title" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "body" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "authorId" INTEGER NOT NULL,

    PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "Article.slug_unique" ON "Article"("slug");

-- AddForeignKey
ALTER TABLE "Article" ADD FOREIGN KEY ("authorId") REFERENCES "User"("id")
    ON DELETE CASCADE ON UPDATE CASCADE;
```

Key observations:
- **PascalCase table names** (Prisma convention for PostgreSQL)
- **camelCase column names** with double quotes
- `SERIAL` (not `BIGSERIAL`) for integer primary keys
- `TIMESTAMP(3)` with millisecond precision
- `DEFAULT CURRENT_TIMESTAMP` for both `createdAt` and `updatedAt`
- `TEXT` for string columns (not VARCHAR)
- Junction tables for many-to-many: `_UserFavorites`, `_UserFollows` (Prisma implicit naming)

### Rollback Pattern
No separate rollback files. Prisma handles rollbacks via `prisma migrate reset` (dev only). There is no production rollback strategy defined — the migration history is append-only.

### Index Strategy
- Unique constraint = unique index (`CREATE UNIQUE INDEX`)
- Foreign keys = implicit index (Prisma's FK `REFERENCES ... ON DELETE CASCADE`)
- Composite unique on junction tables: `CREATE UNIQUE INDEX "_UserFavorites_AB_unique" ON "_UserFavorites"("A", "B")`
- Additional B-tree index on junction table secondary column: `CREATE INDEX "_UserFavorites_B_index" ON "_UserFavorites"("B")`
- No explicit indexes on FK columns (Prisma relies on implicit indexing)
- No partial indexes, GIN/GiST indexes, or BRIN indexes

### Prisma Schema (prisma/schema.prisma)

```prisma
datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

generator client {
  provider        = "prisma-client-js"
  previewFeatures = ["orderByAggregateGroup", "selectRelationCount", "referentialActions"]
}

model Article {
  id          Int       @id @default(autoincrement())
  slug        String    @unique
  title       String
  description String
  body        String
  createdAt   DateTime  @default(now())
  updatedAt   DateTime  @default(now())
  tagList     Tag[]
  author      User      @relation("UserArticles", fields: [authorId], references: [id], onDelete: Cascade)
  authorId    Int
  favoritedBy User[]    @relation("UserFavorites", references: [id])
  comments    Comment[]
}
```

The schema is the **source of truth** for the data model. All field types, relations, and constraints are defined here.

---

## 2. API Routes

### Location and Structure
- **Controllers:** `src/controllers/{entity}.controller.ts` — one file per entity
- **Services:** `src/services/{entity}.service.ts` — one file per entity
- **Route composition:** `src/routes/routes.ts` — assembles all controllers at `/api`
- **Entry point:** `src/index.ts` — mounts `routes`, CORS, body-parser, error handler

### Route Registration
In `src/routes/routes.ts`:
```typescript
const api = Router()
  .use(tagsController)
  .use(articlesController)
  .use(profileController)
  .use(authController);

export default Router().use('/api', api);
```

Each controller file creates its own `Router()` and exports it as the default export.

### Middleware Chain
1. `cors()` — CORS headers (all origins)
2. `bodyParser.json()` — parse JSON request bodies
3. `bodyParser.urlencoded({ extended: true })` — parse URL-encoded bodies
4. Route-level auth middleware: `auth.required` or `auth.optional`
5. Global error handler at bottom of `index.ts`

### Auth Middleware (src/utils/auth.ts)
```typescript
const auth = {
  required: jwt({ secret, getToken, algorithms: ['HS256'] }),
  optional: jwt({ secret, credentialsRequired: false, getToken, algorithms: ['HS256'] }),
};
```
- Token extracted from `Authorization: Token <jwt>` or `Authorization: Bearer <jwt>` header
- `auth.required` — rejects request if no valid JWT
- `auth.optional` — populates `req.user` if JWT present, continues if absent
- `req.user.username` used for authenticated user identity downstream

### Route Handler Pattern (Controller)
```typescript
router.get('/articles', auth.optional, async (req: Request, res: Response, next: NextFunction) => {
  try {
    const result = await getArticles(req.query, req.user?.username);
    res.json(result);
  } catch (error) {
    next(error);
  }
});

router.post('/articles', auth.required, async (req: Request, res: Response, next: NextFunction) => {
  try {
    const article = await createArticle(req.body.article, req.user?.username as string);
    res.json({ article });
  } catch (error) {
    next(error);
  }
});

router.delete('/articles/:slug', auth.required, async (req: Request, res: Response, next: NextFunction) => {
  try {
    await deleteArticle(req.params.slug);
    res.sendStatus(204);
  } catch (error) {
    next(error);
  }
});
```

**Controller rules:**
- Handler is always `async`
- Accepts `(req: Request, res: Response, next: NextFunction)` — all three typed
- Business logic is delegated entirely to service layer (one service call per handler)
- On success: `res.json(data)` or `res.sendStatus(204)` for deletes
- On error: `next(error)` — propagates to global error handler
- No inline validation
- No direct Prisma calls

### Service Pattern
```typescript
export const createArticle = async (article: any, username: string) => {
  const { title, description, body, tagList } = article;

  if (!title) {
    throw new HttpException(422, { errors: { title: ["can't be blank"] } });
  }

  const user = await findUserIdByUsername(username);
  const slug = `${slugify(title)}-${user?.id}`;

  // ...Prisma create...
  return { ...createdArticle, tagList: ..., favoritesCount: ..., favorited: ... };
};
```

**Service rules:**
- Validation throws `HttpException(statusCode, messageOrObject)`
- Complex Prisma queries with `include` and `select` for response shaping
- Returns plain objects (not Prisma types directly) — response mapping happens in service
- Named exports (not default export)
- Prisma client imported from `../../prisma/prisma-client` singleton

### Global Error Handler
```typescript
app.use((err: Error | HttpException, req: Request, res: Response, next: NextFunction) => {
  if (err && err.name === 'UnauthorizedError') {
    return res.status(401).json({ status: 'error', message: 'missing authorization credentials' });
  } else if (err && err.errorCode) {
    res.status(err.errorCode).json(err.message);
  } else if (err) {
    res.status(500).json(err.message);
  }
});
```

### CRUD Route Mapping

| HTTP | Path | Auth | Description |
|---|---|---|---|
| GET | /articles | optional | List articles (paginated, filtered) |
| GET | /articles/feed | required | Personalized feed |
| POST | /articles | required | Create article |
| GET | /articles/:slug | optional | Get single article |
| PUT | /articles/:slug | required | Update article |
| DELETE | /articles/:slug | required | Delete article (204) |
| GET | /articles/:slug/comments | optional | List comments |
| POST | /articles/:slug/comments | required | Add comment |
| DELETE | /articles/:slug/comments/:id | required | Delete comment (204) |
| POST | /articles/:slug/favorite | required | Favorite article |
| DELETE | /articles/:slug/favorite | required | Unfavorite article |

### No Explicit Validation Middleware
Validation is embedded in service functions via `if (!field) throw new HttpException(422, { errors: { field: ["can't be blank"] } })`. There is no Joi, Zod, or express-validator in this codebase.

---

## 3. Tests

### Framework and Config
- **Test runner:** Jest 27.1.0 (run with `jest -i` — sequential, no parallel)
- **TypeScript support:** ts-jest preset
- **Mocking:** jest-mock-extended (`mockDeep<PrismaClient>()`)
- **Config:** `jest.config.js`

```javascript
module.exports = {
  clearMocks: true,
  preset: 'ts-jest',
  testEnvironment: 'node',
  setupFilesAfterEnv: ['<rootDir>/tests/prisma-mock.ts'],
};
```

### Location
Tests live in `tests/` at the project root (not co-located with source). Structure mirrors `src/`:
- `tests/services/article.service.test.ts` ↔ `src/services/article.service.ts`
- `tests/utils/profile.utils.test.ts` ↔ `src/utils/profile.utils.ts`

### Prisma Mock Setup (tests/prisma-mock.ts)
```typescript
import { PrismaClient } from '@prisma/client';
import { mockDeep, mockReset, DeepMockProxy } from 'jest-mock-extended';
import prisma from '../prisma/prisma-client';

jest.mock('../prisma/prisma-client', () => ({
  __esModule: true,
  default: mockDeep<PrismaClient>(),
}));

const prismaMock = prisma as unknown as DeepMockProxy<PrismaClient>;

beforeEach(() => {
  mockReset(prismaMock);
});

export default prismaMock;
```

Auto-loaded via `setupFilesAfterEnv` — no need to import in each test file.

### Test Pattern (Given/When/Then)
```typescript
describe('ArticleService', () => {
  describe('favoriteArticle', () => {
    test('should return the favorited article', async () => {
      // Given
      const slug = 'How-to-train-your-dragon';
      const username = 'RealWorld';
      const mockedUserResponse = { id: 123, username: 'RealWorld', ... };
      const mockedArticleResponse = { id: 123, slug: '...', tagList: [], favoritedBy: [], ... };

      // When
      prismaMock.user.findUnique.mockResolvedValue(mockedUserResponse);
      prismaMock.article.update.mockResolvedValue(mockedArticleResponse);

      // Then
      await expect(favoriteArticle(slug, username)).resolves.toHaveProperty('favoritesCount');
    });

    test('should throw an error if no user is found', async () => {
      // Given
      const slug = 'how-to-train-your-dragon';
      const username = 'RealWorld';

      // When
      prismaMock.user.findUnique.mockResolvedValue(null);

      // Then
      await expect(favoriteArticle(slug, username)).rejects.toThrowError();
    });
  });
});
```

**Test rules:**
- `describe` → `test` nesting (not `it`)
- Given/When/Then comment blocks within each test
- `prismaMock.model.method.mockResolvedValue(...)` for mock setup
- Assertions with `expect(...).resolves.toHaveProperty(...)` or `expect(...).rejects.toThrowError()`
- No database interactions — fully mocked via jest-mock-extended
- Test coverage limited to services and utils (no controller tests in this codebase)

### What Is Not Tested
- Controllers (no controller tests exist)
- Route registration
- Error handler behavior (no integration tests)
- Database schema correctness (no migration tests)

---

## 4. Config

### Environment Variables (.env)
```
DATABASE_URL="postgresql://user:pass@localhost:5432/realworld"
JWT_SECRET="superSecret"
PORT=3000
```

Loaded via Node.js `process.env.*`. No dotenv library explicitly — likely loaded by ts-node-dev or the runtime.

### No Feature Flag System
This codebase has no feature flag system (no LaunchDarkly, no custom flags). All features are always enabled.

### No Permissions System
Auth is binary: authenticated (valid JWT) or not. There is no role-based access control, no permission scopes, no resource-level authorization.

### TypeScript Config (tsconfig.json)
```json
{
  "compilerOptions": {
    "target": "ES5",
    "module": "CommonJS",
    "outDir": "./dist",
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "strict": true
  }
}
```

Key points:
- Strict mode enabled (strictNullChecks, strictFunctionTypes, etc.)
- CommonJS output (not ESM)
- Output to `dist/`

### ESLint Config (.eslintrc.json)
```json
{
  "extends": ["airbnb-base", "prettier"],
  "parser": "@typescript-eslint/parser",
  "parserOptions": { "ecmaVersion": 12, "sourceType": "module" },
  "plugins": ["@typescript-eslint"],
  "rules": {
    "import/extensions": ["error", "ignorePackages", { "ts": "never" }],
    "no-underscore-dangle": ["error", { "allow": ["_count"] }],
    "no-console": ["error", { "allow": ["info"] }]
  }
}
```

Key rules:
- airbnb-base style (no React-specific rules)
- TypeScript parser
- No `.ts` extension in imports
- `_count` (Prisma aggregate field) is allowed by underscore-dangle rule
- Only `console.info` allowed (not `console.log`)

### npm Scripts
```json
{
  "test": "jest -i",
  "dev": "ts-node-dev --poll src/index.ts",
  "start": "node dist/src/index.js",
  "prisma:migrate": "prisma migrate dev",
  "prisma:generate": "prisma generate"
}
```

---

## 5. General Conventions

### Import Style
```typescript
// Third-party (npm)
import express, { NextFunction, Request, Response } from 'express';
import auth from '../utils/auth';
import { createArticle, getArticle } from '../services/article.service';

// No file extensions in imports (enforced by ESLint)
// Relative paths from file location
```

No import ordering enforcement beyond ESLint's `import/order` rule (airbnb-base default).

### Export Style
- **Controllers:** Default export (`export default router;`)
- **Services:** Named exports (`export const createArticle = ...`)
- **Utils:** Default or named depending on usage
- **Models:** Default export for class (HttpException), named for interfaces

### Error Handling Pattern
```typescript
// Custom HttpException class
class HttpException extends Error {
  errorCode: number;
  constructor(errorCode: number, public readonly message: string | any) {
    super(message);
    this.errorCode = errorCode;
  }
}

// Usage in services
throw new HttpException(422, { errors: { title: ["can't be blank"] } });
throw new HttpException(403, { errors: { 'email or password': ['is invalid'] } });
throw new HttpException(404, {});
```

### TypeScript Strictness
- Strict mode on
- `any` used in some places (service function parameters, Prisma query builders)
- Type assertion with `as string` where needed (e.g., `req.user?.username as string`)
- `@ts-ignore` used in the error handler for dynamic type checking

### Prisma Client Singleton
```typescript
// prisma/prisma-client.ts
import { PrismaClient } from '@prisma/client';
const prisma = new PrismaClient();
export default prisma;
```

Imported via relative path `../../prisma/prisma-client` or `../prisma/prisma-client`.

### Response Shaping
Prisma models are not returned directly. Services manually shape responses:
```typescript
// Strip internal fields
const { authorId, id, _count, favoritedBy, ...article } = prisma_result;
// Map nested relations
return {
  ...article,
  author: profileMapper(article.author, username),
  tagList: article.tagList.map(tag => tag.name),
  favoritesCount: _count?.favoritedBy,
  favorited: favoritedBy.some(item => item.username === username),
};
```

### No Audit Logging
No audit log calls anywhere in this codebase. State changes (create, update, delete) are not logged to any audit trail.

### No Vault Integration
Secrets are stored directly in environment variables. There is no Vault integration.

### Slugify Pattern
Articles use a slug computed from the title:
```typescript
const slug = `${slugify(title)}-${user?.id}`;
```
Slugify converts spaces to dashes, lowercases. The user ID suffix makes slugs unique per user.

---

## Acceptance Criteria for Indexer

The automated indexer must extract patterns that match these observations:

| Stage | Expected Source Files | Key Structural Elements |
|---|---|---|
| migrate | `prisma/migrations/**/*.sql` | CREATE TABLE, CREATE UNIQUE INDEX, PascalCase table names, TEXT columns |
| api | `src/controllers/*.controller.ts` + `src/services/*.service.ts` | Router, auth.required/optional, async handler, try/catch/next, Prisma queries |
| tests | `tests/services/*.service.test.ts` | describe/test, Given/When/Then, prismaMock.*.mockResolvedValue, .resolves/.rejects |
| config | `tsconfig.json`, `.eslintrc.json`, `jest.config.js` | Strict mode, CommonJS, airbnb-base, ts-jest preset |

The top-ranked patterns per stage should be the most complete and recent examples. For `api` stage, `article.controller.ts` + `article.service.ts` should rank highest due to CRUD completeness. For `tests`, `article.service.test.ts` should rank highest due to comprehensive Given/When/Then structure.
