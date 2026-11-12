# Storavix v0.2.6 — retired device deletion and UI redesign
- Administrator can delete an obsolete system from its detail view with confirmation.
- Deleted agent IDs are persisted to prevent reappearance from a still-running agent; older agents.json formats migrate automatically.
- Offline SVG icons replace typographic glyphs throughout navigation, summary and system cards.
- Removed "Рабочее пространство" and "Веб-сервер запущен". Original Storavix logo is visually blended into the black interface.
- HTTP setup warning is clarified: warning, not an installation failure. HTTPS remains recommended.
- Added server unit tests for deletion and data migration; added JavaScript syntax CI check.
