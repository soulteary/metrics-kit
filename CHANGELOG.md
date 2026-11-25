# Changelog

## [Unreleased]

## [3.0.8] - 2026-11-26

### Changed

- Update the indirect Fiber adapter dependency `github.com/gofiber/schema`
  from v1.10.0 to v1.10.2.
- Refresh GitHub Actions dependencies used by CI and release validation.

The module path, public API and Go 1.27.4 requirement are unchanged.
Fiber remains in the optional adapter; this patch does not add Fiber
to the root package imports.
