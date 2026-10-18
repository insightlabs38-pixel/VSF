# ADR-028: Schema compatibility and promotion policy

Status: accepted (B02, gate G2). Checker: `node tools/idlgen/src/cli.ts compat`; tests: `tools/idlgen/test/compat.test.ts`.

## Context
Documents outlive the code that wrote them. A change that is safe for new readers can still strand old documents, so every IDL change needs a classification, a version rule and an accountable owner.

## Decisions
1. **Schema-owner gate.** `schemas/ir/**`, `tools/idlgen/**` and `docs/deep/adr/**` are owned in `.github/CODEOWNERS`; changes need the owner's review, and the released snapshot below can only change by adding a new directory.
2. **Released snapshots.** `schemas/ir/released/<version>/` freezes the IDL of each promoted version. CI compares the working IDL with the highest snapshot; the comparison is the only definition of breaking.
3. **Classification.**
   - Additive (minor bump, new fields carry `since` greater than the previous version): new type, new optional field, new `since` field, new enum value, new union variant, widened length, range or list bound, required field made optional.
   - Breaking (major bump): removed or renamed field, type, enum value or variant; changed field type, kind, pattern, tag, text mode, scale or map key; optional made required; required field added without `since`; narrowed bound; moved or removed `since`; changed family root.
   - Prose changes are neutral and keep digests.
4. **Reading rules.** A reader accepts its own major with a minor and patch no newer than itself; newer minors are `schema.too_new`, other majors `schema.incompatible_major`. New enum values and variants therefore only appear in documents that declare the newer version.
5. **Migrations.** A major bump ships a migration edge (`lossless`, `lossy` or `manual`, with a fixture digest) recorded in the version manifest; edges move forward only, and `identity` edges never change version. Lossy and manual migrations require an explicit user-visible status.
6. **Deprecation.** A field is marked deprecated in prose for at least one minor release before a major removal; the removal PR names the replacement and the migration fixture.
7. **Rollback.** A manifest keeps its lineage and the previous promoted digest; rollback re-pins `previous`, never edits a promoted document. A released snapshot is never deleted.
8. **Promotion conditions** for `stable` or `pinned`: every promotion gate passed, the previous digest is in the lineage, retention is consistent (a pinned manifest is immutable, a legal hold has no expiry), the compat check reports no errors, and cross-language fixtures agree.

## Rejected-change examples (all covered by tests)
| Change | Result |
|---|---|
| Remove `unknown` from `Certainty` | `idl.enum_value_removed`, needs major |
| Rename `Limits.max_nodes` | `idl.field_removed`, needs major |
| Make `Attestation.digest` required | `idl.field_became_required`, needs major |
| Add a required field without `since` | `idl.required_field_added`, needs major |
| Shrink `Label.maxLen` | `idl.length_narrowed`, needs major |
| Add an enum value but keep `1.0.0` | `idl.change_without_version_bump` |
| Add a field in `1.1.0` without `since` | `idl.added_field_without_since` |
| Lower `ir_version` | `idl.version_regressed` |

## Consequences
Every schema PR carries its own compatibility verdict. Promotion of 1.0.0 freezes the snapshot at gate G2.

## Revisit when
A consumer outside the monorepo pins an IR version, or a lossy migration is first needed.
