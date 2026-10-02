# Changelog

本文件记录 `pkg/gozip` 的显著变更。

## Unreleased

### 修复

- **压缩结果 `Stat` 失败导致 nil 指针 panic（崩溃级，R1-1）**：原实现 `zipFile.Stat()` 失败仅记日志仍继续执行，
  随后 `fileInfo.Size()` 对 nil 的 `os.FileInfo` 解引用会 panic，直接打挂调用方进程 → 改为 `Stat` 失败时
  大小降级为 0 并输出 `获取压缩文件大小失败` 告警，压缩结果照常返回
  - 影响面：仅 `Stat` 失败（磁盘异常/文件被外部删除）这条罕见路径；正常路径行为不变
  - 守护：`TestZipFilesFromPathsVerifyArchive` 断言 `result.Size` 与磁盘实际大小一致
- **目录压缩丢失子目录结构，同名文件互相覆盖（数据损坏级，R1-2）**：条目名恒取 `filepath.Base`，
  `a/data.txt` 与 `b/data.txt` 在 ZIP 内产生两个同名条目，解压时后者覆盖前者 → 新增内部
  `zipEntry{srcPath, entryName}` 抽象：目录压缩用 `filepath.Rel` + `ToSlash` 生成相对路径条目
  - 影响面：所有 `ZipDirectory` / `ZipDirectoryWithOptions` 调用方，ZIP 内条目名由「基名」变为「相对路径」
    （这是修复目标，属行为变更，见下方「变更」）；`ZipFilesFromPaths*` 条目名保持 `filepath.Base` 不变
  - 守护：`TestZipDirectoryNestedSameNames`（同名文件条目与内容双断言）、`TestZipDirectoryEntryNamesMatchFiles`、
    `TestCollectDirEntries`
- **`ZipOptions.Compression` 从未生效（功能失效级，R1-3）**：`header.Method` 恒为 `zip.Deflate`，
  传 `ZipNoCompression` 也照常压缩 → 新增 `methodForCompression`：`ZipNoCompression` 写 `zip.Store`，其余写
  `zip.Deflate`；同时修正常量定义使语义可表达（见「变更」）
  - 影响面：所有传 `Compression` 的调用方；传 `ZipNoCompression` 的行为由「压缩」变为「仅存储」（即该常量的字面语义）
  - 守护：`TestZipFilesFromPathsWithOptions/不压缩写Store`、`TestMethodForCompression`
- **`ZipOptions.IncludeBaseDir` 从未生效（功能失效级，R1-4）**：字段无任何读取点 → 目录压缩时为 true 则条目
  增加 `filepath.Base(sourceDir)` 前缀
  - 影响面：仅显式设置 `IncludeBaseDir: true` 的调用方（此前该字段是死配置，无人能生效）；默认 false 行为不变
  - 守护：`TestZipDirectoryWithOptions/包含基础目录`、`TestCollectDirEntries/包含基础目录前缀`
- **大文件压缩峰值内存翻倍（性能级，R1-5）**：`os.ReadFile` 整块读入 + `strings.NewReader(string(fileData))`
  再复制一份字符串副本，单文件峰值约 3 倍文件大小 → 改为 `os.Open` + `io.Copy` 流式写入，
  写入字节数由 `io.Copy` 返回值统计
  - 影响面：全部压缩路径；输出字节流不变
  - 守护：`BenchmarkZipFilesFromPathsNoPassword`（基线含 allocs/op，防内存回退）
- **临时文件秒级时间戳并发冲突（并发安全级，R1-6）**：`destPath` 为空时用
  `compress_<unix秒>.zip` 生成路径，同一秒内的并发调用互相覆盖对方文件 → 改用
  `os.CreateTemp("", "gozip-*.zip")` 并复用其句柄
  - 影响面：`destPath` 为空的所有调用方；`result.Path` 由 `compress_<时间戳>.zip` 变为 `gozip-<随机>.zip`
  - 守护：`TestZipFilesFromPathsAutoPathUnique`（8 协程并发生成路径去重断言）
- **压缩失败残留半成品文件（资源泄漏级，R1-7）**：任一环节出错时已创建的目标文件留在磁盘，
  下游可能读到不完整的 ZIP → defer 统一兜底：未成功完成时删除半成品（`os.IsNotExist` 跳过）并告警
  - 影响面：所有失败路径；成功路径不变
  - 守护：`TestZipFilesFromPaths/失败时不残留半成品`
- **zip 写入器关闭时序依赖 defer（正确性风险级，R1-8）**：central directory 的写入错误原先只由 defer 中的
  `Close` 兜底、错误被日志吞掉 → 成功路径显式 `zipWriter.Close()` 并检查错误（`Flush` 无法覆盖该阶段失败），
  再关闭文件；defer 用 `writerClosed/fileClosed/completed` 三个标志避免二次关闭与 `zip: writer closed twice` 噪音
  - 影响面：全部压缩路径；失败判定更严格（写目录失败会返回错误而不是静默产出坏文件）
  - 守护：全部压缩测试的产物都经 `zip.OpenReader` 重新打开验证
- **日志丢失链路上下文与静态检查问题（可维护性级，R1-9）**：`logger.DebugWithCtx(context.Background(), ...)`
  丢 request_id → 改传业务 `ctx`；`fmt.Sprintf("gozip.zip_xxx")` 无格式化参数 → 字符串字面量；
  错误块的三行 span 记录样板抽为 `failSpan`
  - 影响面：仅日志与 Span 记录代码，无行为变更
- **便捷函数与 WithOptions 加密默认值不一致（安全降级陷阱，R1-10）**：`ZipFilesFromPaths` 密码非空默认
  AES-256，而 `ZipOptions.Encryption` 零值是 ZipCrypto——从便捷版迁移到 WithOptions 只想加个
  `Compression`，实际把加密从 AES-256 降级到了 ZipCrypto → 新增 `ZipDefaultEncryption = 0` 作为零值语义，
  `normalizeOptions` 把零值归一为 AES-256（见「变更」R1-C3）
  - 影响面：`WithOptions` 显式传 `&ZipOptions{Password: ...}` 且未指定 `Encryption` 的调用方，
    加密方式由 ZipCrypto 升级为 AES-256（安全方向）；显式指定任何加密类型的行为不变
  - 守护：`TestZipZeroValueEncryptionMatchesConvenience`（两套 API 产物均携带 WinZip AES extra field
    `0x9901`，以 reader 还原 Method 的源码事实为判别依据）、`TestNormalizeOptions/零值加密归一为AES256`
- **未知加密类型静默回退（P1-2，R1-11）**：`getEncryptionMethod` 对 `ZipEncryptionType(999)` 等未知值静默回退
  AES-256，掩盖调用方 bug → `normalizeOptions` 增加枚举校验，未知加密类型返回 `未知的加密类型: <值>`
  （校验先于落盘，不创建目标文件）；`getEncryptionMethod` 的 default 降级为防御层并注释说明
  - 影响面：传非法 `Encryption` 值的调用方（此前静默成功，现在报错——fail loud）
  - 守护：`TestNormalizeOptions/未知加密类型返回错误`、`TestZipFilesFromPathsWithOptionsInvalidEnum`、
    `TestZipDirectoryWithOptions/非法枚举值返回错误`
- **ZipDirectoryWithOptions 重复设置 Span 属性（P1-3，R1-12）**：`writeZipEntries` 已统一设置成功属性与
  `SetStatus(codes.Ok)`，dir.go 又重复一遍（含两个 SetStatus）→ 删除重复块，与
  `ZipFilesFromPathsWithOptions` 一致只信赖核心；同时把 `startTime`（含目录遍历阶段）传入核心，
  `duration_ms` 语义保持「整个 API 耗时」
  - 影响面：仅 Span 属性设置代码；OTel 追加语义下行为不变，消除未来埋点语义冲突风险
- **Compression 非法值静默容忍（P1-4，R1-13）**：`methodForCompression` 对 `-999`/`42` 等任意值静默写
  `zip.Deflate` → 与 P1-2 同路：`normalizeOptions` 校验压缩级别，非法值返回 `未知的压缩级别: <值>`
  - 影响面：传非法 `Compression` 值的调用方（零值与四个合法常量不受影响）
  - 守护：`TestNormalizeOptions/非法压缩级别返回错误`
- **destPath 指向已存在文件时压缩失败会永久丢失原文件（数据丢失级，R2-P1）**：`os.Create(destPath)`
  先把目标截断为 0，随后任一步失败时 R1-7 的 defer `os.Remove` 又把（已截断的）原文件删除——
  「覆盖更新已存在 ZIP」场景下，源文件列表中任一文件不可读（网络挂载抖动/权限变化）即触发，
  原内容无备份地永久丢失 → 改为「同目录临时文件 + 成功后 `os.Rename` 原子替换」
  - 影响面：`destPath` 非空的全部调用方；失败时原文件完好（defer 只清理 `.gozip-*.tmp` 临时文件），
    成功时原子替换、无半成品窗口，与 R1-7「失败不留半成品」语义兼容且更强；产物权限对齐原
    `os.Create` 语义（覆盖保留原权限位，新建文件 0644）；`destPath` 为空的临时文件行为不变；
    新增错误 `重命名临时ZIP文件失败: %w`（仅 rename 失败时，原文件未被触碰）
  - 守护：`TestZipFilesDestPathExisting`（失败不破坏原文件 / 成功原子替换且产物可打开 /
    无 `.gozip-*.tmp` 残留 / 新建文件 0644 权限——最后一条在 Windows 跳过，该平台权限位
    由只读属性模拟，无法区分 0600/0644）

### 变更

- **压缩级别常量数值调整（破坏性，R1-C1）**：底层 `yeka/zip` 未实现 deflate 级别（`writer.go:17`
  `TODO(adg): support specifying deflate level`），而旧定义中 `ZipDefaultCompression` 与 `ZipNoCompression`
  同为 0，语义不可表达（这是 R1-3 长期失效的根因）：
  - `ZipNoCompression`：`0` → `-3`；`ZipBestCompression`：`-3` → `-2`；`ZipDefaultCompression = 0`、
    `ZipBestSpeed = -1` 不变
  - 迁移方式：一律引用常量名而非字面量数值（仓内 grep 确认无字面量依赖，无调用方需要改动）；
    传 `ZipNoCompression` 的调用方将首次获得「仅存储」的真实行为
- **目录压缩条目名由基名改为相对路径（R1-C2）**：见 R1-2；解压后目录结构与源目录一致，
  条目名统一正斜杠
- **加密类型常量数值调整（破坏性，R1-C3）**：为让零值表达「使用默认（AES-256）」，新增
  `ZipDefaultEncryption = 0`，其余常量顺移：`ZipStandardEncryption` `0`→`1`、`ZipAES128Encryption` `1`→`2`、
  `ZipAES192Encryption` `2`→`3`、`ZipAES256Encryption` `3`→`4`
  - 这是 R1-10 的必要前提：旧定义中零值就是 ZipCrypto，无法区分「未设置」与「显式选 ZipCrypto」
  - 迁移方式：一律引用常量名而非字面量数值（仓内 grep 确认无字面量依赖）；显式传
    `ZipStandardEncryption` 仍得 ZipCrypto，零值调用方从 ZipCrypto 升级为 AES-256（安全方向）
- **非法枚举值由静默容忍变为返回错误（行为变更，随 R1-11/R1-13 修复）**：未知 `Encryption` 与
  非法 `Compression` 之前静默走默认分支，现在返回中文错误且不落盘
- **开始压缩日志由 INFO 降为 Debug（P2-5）**：`writeZipEntries` 的「开始ZIP压缩」是每次压缩必经的热路径，
  大批量场景下刷 INFO 造成日志噪音 → 降为 `DebugWithCtx`；「ZIP压缩完成」仍为 INFO（承载体积/耗时统计）
- **私有函数 `calculateCompressionRatio` 更名 `calculateCompressedPercent`（P2-4）**：函数名明确返回语义为
  「压缩后占原文的百分比，值越低越小」；日志字段名 `compression_ratio` **保持不变**，不破坏下游日志解析
- **文档合并**：`ZIP_USAGE.md` 删除，全部内容并入 `README.md`（单一事实单一来源；旧文件含乱码标题与
  `tools.ZipFilesFromPaths` 等已失效示例）
- **测试不再向包目录写产物**：删除 `TestGenerateLocalZipFiles` / `TestZipUsageMarkdown`
  （往 `pkg/gozip/files/` 生成示例 ZIP，污染 `git status`），其演示价值由 README 示例承担

### 新增

- **`ZipOptions.TempDir` 字段（P2-1）**：指定自动生成临时文件的目录，仅 `destPath` 为空时生效；
  空值保持系统默认临时目录（零值行为不变）。由 `TestZipFilesFromPathsWithOptions/指定临时目录`（路径前缀
  断言）与 `.../TempDir不存在返回错误` 守护
- **`README.md`（文档）**：架构概览、职责边界、三个使用场景、参数与常量表（含级别常量限制的源码证据）、
  API 速查、错误处理表、性能基线与模糊测试
- **`CHANGELOG.md`（文档）**：本文件
- **`benchmark_test.go`（测试）**：`BenchmarkZipFilesFromPathsNoPassword` / `...WithAES256` /
  `BenchmarkZipDirectory` 三条本地回归基线，读数已录入 README
- **`fuzz_test.go`（测试）**：`FuzzZipRoundTrip` 守护三条不变量（条目名逐字节一致、UTF-8 标志位保留、
  内容往返一致），种子语料随常规 `go test` 执行
- **测试补齐**：`TestZipFilesFromPathsAutoPathUnique`（并发临时路径唯一性）、
  `TestZipFilesFromPaths/失败时不残留半成品`、`TestZipDirectoryNestedSameNames`（同名文件守护）、
  `TestCollectDirEntries`、`TestMethodForCompression`、`TestZipDirectory/空路径返回错误`
- **R1 终审收口测试**：`TestNormalizeOptions`（零值归一 / 合法透传 / 非法报错 / 不改调用方结构体七子测试）、
  `TestZipZeroValueEncryptionMatchesConvenience`（加密强度不降级守护）、
  `TestZipFilesFromPathsWithOptionsInvalidEnum` 与 `TestZipDirectoryWithOptions/非法枚举值返回错误`
  （fail loud 且不落盘）、`hasWinZipAESExtra` 判别 helper（0x9901 extra field 遍历）、
  临时路径 `gozip-` 前缀断言；`zipEntryNames` 参数改 `testing.TB` 与其余 helper 对齐
- **P2 收口**：`指定临时目录` / `TempDir不存在返回错误` 子测试；`TestZipLargeFile` 阈值附实测证据
  （本机 0.31%，1,080,000 → 3,400 bytes，距 50% 阈值 160 倍余量，非机器敏感阈值）；
  `TestCalculateCompressionRatio` 随函数更名 `TestCalculateCompressedPercent`

### 兼容性说明

- 导出 API 签名零变化：`ZipFilesFromPaths`、`ZipFilesFromPathsWithOptions`、`ZipDirectory`、
  `ZipDirectoryWithOptions`、`ZipOptions`、`ZipFileResult`、全部常量名保持不变
- 破坏性点是 R1-C1（压缩级别常量数值）与 R1-C3（加密类型常量数值）两次数值调整——符号引用的
  调用方均无需改动；另 R1-10 使零值加密从 ZipCrypto 升级为 AES-256、非法枚举值从静默改为报错
- `requestIDAttr` 的 attribute key 维持 `gozip.request_id`，与 `request-id-propagation` skill 登记的
  gozip 行一致；Span 名由 `fmt.Sprintf` 改为字面量后字符串内容不变
- 包结构按 `package-quality-baseline` 收口为「源文件 ↔ 测试文件一对一」，未新增集成测试分层
  （压缩仅依赖本地文件系统，无外部服务）
- R2-P1 后 `destPath` 非空时内部经 `.gozip-*.tmp` 中转再 rename——临时文件与目标同目录，
  同一文件系统内原子替换，不存在跨设备 rename 问题；导出 API 签名与 `ZipFileResult.Path` 语义不变
  （仍返回用户传入的 `destPath`）
