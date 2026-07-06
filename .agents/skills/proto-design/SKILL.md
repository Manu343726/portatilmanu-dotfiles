---
name: proto-design
description: Protobuf RPC design patterns for dotfilesd plugins. Use this skill when creating or modifying .proto files or implementing Connect RPC handlers. Ensures type-safe, self-documented APIs with enums instead of strings, repeated instead of comma-separated fields, and proper message grouping.
---

# Protobuf RPC Design

**Read `~/dotfilesd/docs/proto-design.md` before designing any proto file.**

## Quick rules

1. **Strings for options** → use `enum` instead
2. **Comma-separated lists** → use `repeated` instead
3. **Flat field soup** → group into sub-messages (`Filter`, `Display`, etc.)
4. **Missing comments** → document every field
5. **`string output`** → use `OutputFormat` enum
