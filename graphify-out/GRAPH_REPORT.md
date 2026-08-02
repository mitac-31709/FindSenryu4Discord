# Graph Report - FindSenryu4Discord  (2026-08-02)

## Corpus Check
- 64 files · ~49,252 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 654 nodes · 1791 edges · 26 communities (17 shown, 9 thin omitted)
- Extraction: 70% EXTRACTED · 30% INFERRED · 0% AMBIGUOUS · INFERRED: 544 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `6178c5fe`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- senryu_test.go
- admin.go
- buildRescanResult
- main.go
- FindWithOpt
- Init
- IsChannelTypeEnabled
- Manager
- Info
- delete_test.go
- setupDetectionTestDB
- welcome_test.go
- Server
- ImportChannelHistory
- Config
- logger.go
- HandleDoctorCommand
- github.com/0x307e/go-haiku
- ApplicationCommand
- Channel
- GuildCreate
- InteractionCreate
- Context
- Guild
- Location
- T

## God Nodes (most connected - your core abstractions)
1. `setupSenryuTestDB()` - 40 edges
2. `Info()` - 32 edges
3. `RecordDatabaseOperation()` - 32 edges
4. `RecordError()` - 30 edges
5. `Warn()` - 26 edges
6. `FindWithOpt()` - 25 edges
7. `CreateSenryu()` - 24 edges
8. `Senryu` - 20 edges
9. `respondError()` - 20 edges
10. `Config` - 19 edges

## Surprising Connections (you probably didn't know these)
- `SendWelcomeMessage()` --calls--> `RecordWelcomeMessageSent()`  [INFERRED]
  commands/welcome.go → pkg/metrics/metrics.go
- `CreateSenryu()` --calls--> `RecordSenryuDetected()`  [INFERRED]
  service/senryu.go → pkg/metrics/metrics.go
- `main()` --calls--> `Load()`  [INFERRED]
  cmd/migrate/main.go → config/config.go
- `main()` --calls--> `Info()`  [INFERRED]
  cmd/migrate/main.go → pkg/logger/logger.go
- `handleImportCommand()` --calls--> `ImportChannelHistory()`  [INFERRED]
  commands/admin.go → service/import.go

## Import Cycles
- None detected.

## Communities (26 total, 9 thin omitted)

### Community 0 - "senryu_test.go"
Cohesion: 0.09
Nodes (79): GuildChannelTypeSetting, Metadata, MutedChannel, Senryu, Time, YomeEvent, Warn(), RecordDatabaseOperation() (+71 more)

### Community 1 - "admin.go"
Cohesion: 0.07
Nodes (73): AdminCommands(), allGuilds(), canManageChannel(), floatPtr(), getUserID(), ApplicationCommand, ApplicationCommandInteractionDataOption, Guild (+65 more)

### Community 2 - "buildRescanResult"
Cohesion: 0.07
Nodes (52): buildRescanResult(), collectSavableMatches(), formatAuthorPlain(), ApplicationCommand, InteractionCreate, MessageComponent, MessageEmbed, Session (+44 more)

### Community 3 - "main.go"
Cohesion: 0.09
Nodes (53): ApplicationCommand, Channel, Connect, GuildCreate, InteractionCreate, buildTankaMessage(), buildUserCommands(), clearStaleGuildUserCommands() (+45 more)

### Community 4 - "FindWithOpt"
Cohesion: 0.15
Nodes (39): Dict, Opt, contains(), countChars(), dictIdx(), Find(), FindWithOpt(), Writer (+31 more)

### Community 5 - "Init"
Cohesion: 0.13
Nodes (36): main(), Close(), GetDB(), GetStats(), DB, Init(), initDB(), IsConnected() (+28 more)

### Community 6 - "IsChannelTypeEnabled"
Cohesion: 0.15
Nodes (33): buildChannelSettingsResponse(), ChannelType, InteractionCreate, Session, HandleChannelCommand(), HandleChannelToggle(), channelTypeInfo, InteractionResponseData (+25 more)

### Community 7 - "Manager"
Cohesion: 0.10
Nodes (22): Manager, Context, Duration, Guild, Location, main(), durationUntilNextMidnightJST(), formatCount() (+14 more)

### Community 8 - "Info"
Cohesion: 0.16
Nodes (22): BackupInfo, Manager, BackupConfig, copyFile(), Context, Time, NewManager(), Info() (+14 more)

### Community 9 - "delete_test.go"
Cohesion: 0.17
Nodes (30): buildDeletePage(), componentsToSlice(), deleteJST(), Location, MessageComponent, Time, parseDeleteDateRange(), T (+22 more)

### Community 10 - "setupDetectionTestDB"
Cohesion: 0.25
Nodes (27): DetectionOptOut, AdminBanDetection(), DeleteOptOutByServer(), IsAdminBanned(), IsDetectionOptedOut(), ListOptOutsByServer(), OptInDetection(), optOutCacheKey() (+19 more)

### Community 11 - "welcome_test.go"
Cohesion: 0.16
Nodes (25): buildWelcomeEmbed(), ClearGuildWelcomeSent(), Guild, GuildCreate, MessageEmbed, Session, hasChannelSendPermission(), isPermanentSendError() (+17 more)

### Community 12 - "Server"
Cohesion: 0.10
Nodes (12): HealthResponse, Server, Context, Time, NewServer(), StartServer(), RecordSenryuDetected(), RecordWelcomeMessageSent() (+4 more)

### Community 13 - "ImportChannelHistory"
Cohesion: 0.15
Nodes (18): IsExcludedSenryu(), IsExcludedSenryuParts(), T, TestIsExcludedSenryu(), TestIsExcludedSenryuParts(), Session, ImportChannelHistory(), isSourceBot() (+10 more)

### Community 14 - "Config"
Cohesion: 0.12
Nodes (15): AdminConfig, Config, boolPtr(), Load(), setDefaults(), T, TestSetDefaults_TankaReaction(), TestSetDefaults_YomeMax() (+7 more)

### Community 15 - "logger.go"
Cohesion: 0.27
Nodes (14): Level, Config, Debug(), DebugContext(), Error(), ErrorContext(), Context, Writer (+6 more)

### Community 16 - "HandleDoctorCommand"
Cohesion: 0.24
Nodes (9): channelDisplayName(), channelTypeName(), Channel, ChannelType, InteractionCreate, Session, HandleDoctorCommand(), requiredPermission (+1 more)

## Knowledge Gaps
- **13 isolated node(s):** `MutedChannel`, `GuildChannelTypeSetting`, `Metadata`, `requiredPermission`, `DatabaseConfig` (+8 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **9 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Warn()` connect `senryu_test.go` to `admin.go`, `buildRescanResult`, `Init`, `Info`, `welcome_test.go`, `ImportChannelHistory`, `logger.go`, `HandleDoctorCommand`?**
  _High betweenness centrality (0.239) - this node is a cross-community bridge._
- **Why does `Info()` connect `Info` to `senryu_test.go`, `admin.go`, `main.go`, `Init`, `IsChannelTypeEnabled`, `setupDetectionTestDB`, `welcome_test.go`, `Server`, `ImportChannelHistory`, `logger.go`?**
  _High betweenness centrality (0.163) - this node is a cross-community bridge._
- **Why does `Senryu` connect `senryu_test.go` to `setupDetectionTestDB`, `main.go`, `Init`?**
  _High betweenness centrality (0.143) - this node is a cross-community bridge._
- **Are the 30 inferred relationships involving `Info()` (e.g. with `.cleanupOldBackups()` and `.CreateBackup()`) actually correct?**
  _`Info()` has 30 INFERRED edges - model-reasoned connections that need verification._
- **Are the 31 inferred relationships involving `RecordDatabaseOperation()` (e.g. with `DeleteChannelConfigByGuild()` and `GetGuildChannelSettings()`) actually correct?**
  _`RecordDatabaseOperation()` has 31 INFERRED edges - model-reasoned connections that need verification._
- **Are the 29 inferred relationships involving `RecordError()` (e.g. with `DeleteChannelConfigByGuild()` and `SetChannelTypeEnabled()`) actually correct?**
  _`RecordError()` has 29 INFERRED edges - model-reasoned connections that need verification._
- **What connects `MutedChannel`, `GuildChannelTypeSetting`, `Metadata` to the rest of the system?**
  _13 weakly-connected nodes found - possible documentation gaps or missing edges._