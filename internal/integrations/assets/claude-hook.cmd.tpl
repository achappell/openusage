@echo off
rem openusage-integration-version: __OPENUSAGE_INTEGRATION_VERSION__
if /I "%OPENUSAGE_TELEMETRY_ENABLED%"=="false" exit /b 0
if /I "%OPENUSAGE_TELEMETRY_ENABLED%"=="0" exit /b 0
"__OPENUSAGE_BIN_DEFAULT__" telemetry hook claude_code 1>nul 2>nul
