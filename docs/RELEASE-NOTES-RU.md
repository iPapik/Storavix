# Storavix v0.2.3 — Windows agent installer hotfix

- Fix PowerShell 5.1 online installer crash: RuntimeInformation.OSArchitecture may be null on older .NET Framework versions.
- Detect native CPU architecture from PROCESSOR_ARCHITEW6432 / PROCESSOR_ARCHITECTURE with CIM fallback (AMD64, ARM64).
- Test architecture detection using actual Windows PowerShell 5.1 in GitHub Actions, in addition to parser checks.
- No agent token or server data migration is required. Users can rerun the same PowerShell online installation command.

Other agent collection features and dashboard functions are unchanged from v0.2.2.
