# codex-sync

[English](README.md) | 简体中文

将 Codex 的本地会话加密同步到另一台 Mac，方便在不同设备上继续对话。支持 Cloudflare R2、AWS S3、Google Cloud Storage、S3 兼容服务、WebDAV，以及 **iCloud Drive 共享文件夹**。

本仓库是 [d-jiao/codex-sync](https://github.com/d-jiao/codex-sync) 的分支，新增了 iCloud 共享文件夹支持。项目最初源自 [claude-sync](https://github.com/tawanorg/claude-sync)，归属说明见 [NOTICE](NOTICE)。

- **上传前加密**：文件先在本地压缩，再使用 age 加密。
- **保留冲突**：同一会话在两端发生修改时保留双方副本，供你选择。
- **账号分离**：不同步 `auth.json`、`installation_id` 或本地数据库。iCloud 模式还强制排除配置、规则、技能和记忆文件。
- **存储由你选择**：使用自己的云存储，或明确邀请另一 Apple 账户加入的 iCloud 文件夹。

这是早期版本的软件。iCloud 功能已通过自动化模拟测试，但尚未完成真实双 Mac、不同 Apple ID 的联机验收；正式使用前请先用数据副本验证。

## 目录

- [安装](#安装)
- [iCloud：两台 Mac、不同 Apple ID 和 Codex 账号](#icloud两台-mac不同-apple-id-和-codex-账号)
- [其他存储服务](#其他存储服务)
- [日常命令](#日常命令)
- [同步范围与账号隔离](#同步范围与账号隔离)
- [同步与冲突处理](#同步与冲突处理)
- [在桌面应用中显示会话](#在桌面应用中显示会话)
- [iCloud 故障恢复](#icloud-故障恢复)
- [安全与限制](#安全与限制)
- [开发与测试](#开发与测试)

## 安装

主要面向 macOS。源码构建需要 Go 1.24 或更新版本，以及 `make`。

要使用此分支新增的 iCloud 功能，请从本仓库构建：

```sh
git clone https://github.com/Zeen-Wang/codex-sync.git
cd codex-sync
make build
make install
```

`make build` 生成 `bin/codex-sync`；`make install` 默认安装到 `~/.local/bin/codex-sync`。确保 `~/.local/bin` 已加入 `PATH`，或者直接使用 `./bin/codex-sync`。

```sh
codex-sync --version
codex-sync init --help
```

如果已经克隆到 `~/git/codex-sync`，直接进入该目录构建，无需重新克隆。

**此分支的更新方式**：确认自己的改动已妥善保存，再运行：

```sh
git pull --ff-only
make build
make install
```

当前代码中的 `codex-sync update` 和 `changelog` 仍访问上游 `d-jiao/codex-sync` 的 Releases。不要依赖 `update` 保留本分支新增的 iCloud 功能。

## iCloud：两台 Mac、不同 Apple ID 和 Codex 账号

### 1. 明确共享一个专用文件夹

1. 在 Mac A 的 iCloud Drive 中创建一个空文件夹，例如 `Codex Sessions`。
2. 在 Finder 中将它共享给 Mac B 使用的 Apple 账户，选择“仅受邀用户”和“可更改”权限。
3. 在 Mac B 上接受邀请，确认打开的是同一个共享文件夹，而不是名称相同的本地副本。
4. 先放入一个测试文件，验证 A 的修改能到 B、B 的修改也能到 A。删除测试文件，等待两端目录都变空。
5. 在 Finder 中下载完整文件夹；如果系统提供“保持下载”选项，请启用。

**iCloud 家庭共享或共用存储套餐不会自动共享这个文件夹，仍需单独邀请。** 两台 Mac 的本地路径可以不同，初始化时分别填写本机的实际路径。可在 Finder 中按住 Option 右键，复制文件夹路径。

不要把 `~/.codex` 或 `~/.codex-sync` 本身放进共享文件夹，也不要将它们符号链接到 iCloud。

### 2. 在 Mac A 上初始化并首次上传

先独立备份本机 Codex 数据和 `~/.codex-sync`。同步前退出 Codex 及其他会话写入进程，并让两台机器使用相同的 Codex 版本。

```sh
codex-sync init --provider icloud \
  --shared-folder "$HOME/Library/Mobile Documents/com~apple~CloudDocs/Codex Sessions"
```

这里的路径只是示例，必须替换为本机实际的共享文件夹路径。

向导会：

- 检查目录是否存在、是否为空或已初始化，以及是否与本地数据、密钥或同步状态重叠。
- 设置加密密钥。建议选择 **Passphrase**，使用至少 12 个字符的强口令。
- 检查本地可见的远端文件能否用该密钥解密。
- 将同步范围固定为 `sessions`。

这套加密口令独立于 Apple 密码和 Codex 登录。随后上传：

```sh
codex-sync push
```

等待 Finder 上传完成，并确认 Mac B 已下载对应文件，再初始化 Mac B。

### 3. 在 Mac B 上初始化并拉取

使用 **Mac B 上的共享文件夹路径**，输入与 Mac A **相同的加密口令**：

```sh
codex-sync init --provider icloud --shared-folder "/本机共享文件夹的绝对路径"
codex-sync pull --dry-run
codex-sync pull
```

如果本机已有会话，第一次拉取会提供备份、覆盖或取消选项。建议先预览并选择备份。

如果共享文件尚未到达 B，向导看到空目录时无法确认口令是否与 A 一致；因此不要跳过首次上传和等待下载的步骤。

本机配置示例：

```yaml
storage:
  provider: icloud
  shared_folder: /Users/你的用户名/Library/Mobile Documents/com~apple~CloudDocs/Codex Sessions
scope: sessions
encryption_key_path: ~/.codex-sync/age-key.txt
```

配置文件、密钥和 `state.json` 都保留在各自机器上。不要复制另一台机器的 `state.json`。

若从其他存储服务迁移，先备份原配置、密钥和状态；完成新配置后，将旧 `state.json` 移到本地备份位置，让新后端重新建立状态。已有配置时，`init --passphrase` 只用于重新输入口令，不能用来切换存储服务。

### 4. 轮流使用两台 Mac

每次交接按以下顺序操作：

1. 当前机器结束工作，退出 Codex，运行 `codex-sync push`。
2. 等待当前机器上传、另一台机器下载完成。
3. 在另一台机器上先运行 `codex-sync pull --dry-run`，检查后再运行 `codex-sync pull`。
4. 确认会话内容正确后，再打开 Codex 继续工作。

**一次只使用一台 Mac。** `push` 成功只表示写入了本地 iCloud 副本，不表示另一台机器已收到数据。iCloud 是最终一致的文件同步系统，没有跨 Mac 分布式锁，也没有“等待全云同步完成”的保证。

如果以前安装了本项目的定时同步，在两台机器的仓库目录中分别停用：

```sh
make uninstall-launchd
```

iCloud 模式不应依赖双机自动定时推拉来避免冲突。

### iCloud 模式的保护措施

- 加密内容以不可变历史版本保存，不直接覆盖旧版本。
- 上传前比较本地可见的远端版本，拒绝过时的写入。
- 检测到并发分叉、缺失历史、损坏记录或异常云端冲突副本时停止，不按时间戳自动选赢家。
- 对已知但暂时不可见的版本，阻止回退到其旧版本。
- 本机锁防止同一 Mac 上同时运行推拉，但不能锁住另一台 Mac，也不能阻止 Codex 写会话。
- **不会因为远端暂时看不到文件就删除本地文件**。
- **禁止 `push --force` 和 `reset --remote`**，不提供远端删除或自动清理历史。

代价是历史会占用空间，归档或删除的会话可能在不同位置保留副本。不要手动删除仍在使用的共享目录中的祖先版本。更详细的格式、限制和验收步骤见 [iCloud 文档（英文）](docs/icloud.md)。

## 其他存储服务

| Provider | 需要准备 |
|---|---|
| `r2` | Cloudflare R2 存储桶、Account ID、Access Key ID、Secret Access Key |
| `s3` | AWS S3 存储桶、Region、访问密钥 |
| `gcs` | GCP Project ID、存储桶、服务账号凭据或默认凭据 |
| `s3-compatible` | S3 兼容端点、存储桶、访问密钥，例如 MinIO、Backblaze B2、Wasabi |
| `webdav` | WebDAV URL、用户名、应用专用密码，例如 Nextcloud、ownCloud |
| `icloud` | 已接受邀请、可编辑且已下载的专用共享文件夹 |

费用和配额以各服务商当前说明为准。

一般通过交互向导设置：

```sh
codex-sync init
codex-sync push
```

另一台机器选择相同的存储位置和加密口令，然后预览并拉取。非 iCloud 后端可以选择 `full` 或 `sessions` 范围。

自定义 S3 端点示例：

```sh
codex-sync init --provider s3-compatible --endpoint https://s3.example.com
```

如果服务要求 `endpoint/bucket/key` 形式的路径寻址，增加 `--use-path-style`。

非 iCloud 后端可按需安装每日任务：

```sh
make install-launchd
```

该任务安装后立即执行，并在每日 03:00 和登录时执行 `pull -q && push -q`，日志位于 `~/Library/Logs/codex-sync.log`。任务不使用 `push --force`。若使用自定义 Codex 数据目录，应在安装任务时通过 `CODEX_HOME` 指定。

## 日常命令

| 命令 | 用途 |
|---|---|
| `codex-sync init` | 设置存储、加密和同步范围 |
| `codex-sync push` | 上传本地变化 |
| `codex-sync pull --dry-run` | 预览拉取 |
| `codex-sync pull` | 拉取远端变化 |
| `codex-sync pull --desktop` | 拉取成功后刷新桌面索引，需先退出应用 |
| `codex-sync desktop refresh` | 单独刷新桌面索引 |
| `codex-sync status` | 查看本地待同步变化 |
| `codex-sync diff` | 比较本地与远端 |
| `codex-sync conflicts` | 查看并处理本地冲突副本 |
| `codex-sync paths` | 查看同步路径和排除规则 |
| `codex-sync trash list` | 查看普通云存储后端的远端回收副本 |
| `codex-sync trash restore <batch>` | 恢复指定批次的远端回收副本 |
| `codex-sync icloud export <revision> --output <file>` | 将保留的 iCloud 版本导出为恢复文件 |

可使用 `-q` 减少输出；文件错误仍会输出，推拉有失败时返回非零退出码。

注意区分这些选项：

- `pull --force`：跳过首次拉取确认，不是解决冲突的“强制覆盖”开关。
- `pull --no-delete`：不移动远端已消失的本地文件；iCloud 始终采用不删除策略。
- `push --force`：普通后端可删除远端对应文件；iCloud 不支持。
- `reset`：删除本地工具配置、密钥和同步状态，保留本地回收目录，不删除 Codex 会话。操作前务必保存仍需使用的密钥。
- `reset --remote`：普通后端还会尝试清空远端；iCloud 拒绝该操作并保留本地配置、密钥和状态。

## 同步范围与账号隔离

Codex 数据目录默认是 `~/.codex`，可通过 `CODEX_HOME` 修改；工具自己的配置和状态位于 `~/.codex-sync`。

| 数据 | `sessions` | `full` |
|---|---|---|
| `sessions/`、`archived_sessions/` | 是 | 是 |
| `session_index.jsonl` 会话名称 | 是 | 是 |
| `history.jsonl` 输入历史 | 是 | 是 |
| `attachments/` 附件 | 是 | 是 |
| `config.toml` | 否 | 是 |
| `rules/`、`skills/`、`memories/`、`AGENTS.md` | 否 | 是 |

iCloud 强制使用会话范围，即使手动配置 `scope: full` 或 `sync_paths: ["."]` 也不会扩大到配置、规则等内容。

登录凭据 `auth.json`、设备标识 `installation_id`、SQLite/DB 数据库、缓存、日志、插件包、工作树和 `.conflict.*` 冲突副本均被排除。完整规则以 [配置实现](internal/config/config.go) 为准。

自定义排除示例：

```yaml
exclude:
  - "*.tmp"
  - "attachments/**"
```

```sh
codex-sync paths exclude 'attachments/**'
codex-sync paths unexclude 'attachments/**'
```

会话内容中的用户主目录路径在上传时转换为 `${HOME}`，拉取时还原为接收方的目录。若项目位置不只是用户名不同，可在两台机器各自的配置中指定同名路径标记：

```yaml
# Mac A
path_map:
  ~/work: WORK
```

```yaml
# Mac B
path_map:
  ~/Projects: WORK
```

项目源码需要自行通过 Git 等方式同步。会话同步不会共享另一 Codex 账号的云端权限、订阅或工作区访问权，也不会完整复制 Projects/侧边栏分组。

## 同步与冲突处理

`push` 根据内容哈希上传变化的文件。有未解决的 `.conflict.*` 副本时会跳过对应原文件并报错，防止覆盖另一端内容。

普通后端默认保留本地已删除文件的远端副本。显式执行 `push --force` 时，先将待删除对象复制到远端 `_trash/<batch>/`，再进行版本检查和删除；复制失败则不删除。版本检查能力取决于存储服务，不能保证跨设备并发绝对安全。

`pull` 的主要行为：

- 下载新文件和远端变化。
- 两端都修改过的会话：保留本地文件，将远端内容写入 `<file>.conflict.<timestamp>`。
- `session_index.jsonl` 与 `history.jsonl`：按既定规则合并，后续推送发布合并结果。
- 普通后端中，已跟踪且本地未修改、但远端已消失的文件，会移入 `~/.codex-sync/trash/<timestamp>/`；`--no-delete` 可关闭。**iCloud 不执行此步骤。**
- 空远端不会触发本地删除。写入使用临时文件和原子替换，并拒绝不安全路径及符号链接。

处理普通冲突：

```sh
codex-sync conflicts --list
codex-sync conflicts
```

交互界面支持保留本地、保留远端、查看差异或跳过。批量选择前先检查内容：

```sh
codex-sync conflicts --keep local
# 或选择远端：
codex-sync conflicts --keep remote
```

首次向已有数据的机器拉取时，可以先备份到 `<Codex 数据目录>.backup.<时间戳>`。这份备份只覆盖同步集合，**不包括登录文件和数据库**，不能代替完整的本地备份。

## 在桌面应用中显示会话

若文件已拉取但桌面应用没有显示对应会话，可在完全退出相关桌面应用及 `codex` 进程后执行：

```sh
codex-sync desktop refresh
# 或在拉取时一起刷新：
codex-sync pull --desktop
```

现有实现使用桌面应用的引擎重建会话索引、补充名称并更新本地目录状态，默认先备份数据库和会话名称索引到 `~/.codex-sync/db-backup-<时间戳>/`。

检测到应用或引擎仍运行时会拒绝刷新。`pull --desktop` 可能已经完成文件拉取，再因刷新被拒绝而返回错误；关闭应用后单独刷新即可。

桌面刷新依赖具体应用、引擎版本和本地数据库结构，并非对所有版本的兼容保证。两端应使用相同版本，并先在数据副本上验证。数据库和侧边栏分组本身不跨设备同步。

## iCloud 故障恢复

### 下载未完成、缺少历史或损坏记录

先停止交替写入，在 Finder 中等待并下载完整目录，然后重试。不要通过删除报错的祖先文件或云端冲突副本来绕过检查。

一份尚未收到新版本、但内部仍然自洽的旧副本，可能无法被程序识别为过时。因此仍需遵循“等待下载，再拉取，再编辑”的顺序。

### 本机锁未释放

程序意外退出可能留下锁。先确认本机所有 `codex-sync` 进程都已结束，再只删除错误信息指出的空锁目录。程序不会因为超时而自动抢锁。

### `iCloud concurrent revisions`

这是共享历史分叉，不等同于普通 `.conflict.*` 文件：

1. 停止两台机器上的写入及定时任务，备份双方本地数据和完整共享目录。
2. 等待文件尽可能完整地到达。错误信息会列出对象名及两个版本 ID。
3. 将双方版本分别导出到 Codex 数据目录和共享目录之外的新文件：

   ```sh
   codex-sync icloud export REVISION_ID \
     --output /绝对路径/恢复目录/branch-a.jsonl
   ```

4. 导出内容已解密、解压，不覆盖已有文件，权限为 `0600`，保留 `${HOME}` 等可移植路径标记。根据打印的对象名判断它是会话还是元数据清单。
5. 在独立恢复副本中检查、合并；不要盲目拼接不同的会话延续。
6. 保留旧共享目录作为恢复历史，使用新空共享目录和原密钥重新建立同步。先备份并移走两端旧同步状态，由整理好的源机器上传，等待后再让另一台预览、拉取。

不会自动按时间戳选择赢家，也不会自动清理分叉。详细流程见 [docs/icloud.md](docs/icloud.md)。

### 加密口令错误或遗忘

iCloud 初始化先验证临时密钥，验证失败会保留原密钥。可重新输入正确口令：

```sh
codex-sync init --passphrase
```

如果遗忘口令但仍有可用的 `age-key.txt`，应妥善保存该密钥，它仍可用于解密。若口令和密钥均丢失，就不能解密远端数据；只能从仍保存的本地明文副本重新开始。

## 安全与限制

- 文件先 gzip 压缩，再使用 age 加密；口令通过 Argon2id 派生密钥，固定盐保持跨设备兼容。
- 文件名、对象名和时间等元数据并非完全隐藏。
- 登录隔离不代表会话没有敏感内容：提示词、工具输出和附件仍可能包含秘密；拥有共享数据和密钥的人可以读取这些内容。
- 本地配置中的存储凭据以明文保存，应保护 `~/.codex-sync` 和备份。
- 不同步项目源码、工作树、自动化配置、侧边栏组织或记忆数据库。
- 不应在两台机器上同时继续同一会话；iCloud 尤其不支持可靠的离线多写入者协作。
- iCloud 历史不会自动回收，长期使用会增加存储和扫描成本。
- 桌面刷新与 launchd 功能面向 macOS；其他平台没有等同的兼容承诺。

更多说明见 [安全模型](docs/security.md)、[安全审计](SECURITY-AUDIT.md) 和 [漏洞报告方式](SECURITY.md)。

## 开发与测试

```sh
make build
make test
make check
```

`make check` 检查格式、运行 `go vet` 和短测试。新增 iCloud 测试覆盖独立账号文件、加密传输、过时写入、分叉保留、乱序到达、缺失记录、路径保护、本机锁及删除拦截。

开发验证应使用隔离的临时数据目录和存储位置，不要让开发构建直接操作真实的 `~/.codex` 或正式同步目录。

请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)、[AGENTS.md](AGENTS.md)、[架构说明](CLAUDE.md) 和 [iCloud 设计说明](docs/specs/2026-10-10-icloud.md)。

## 许可证

MIT，见 [LICENSE](LICENSE)。上游归属和分支来源见 [NOTICE](NOTICE)。
