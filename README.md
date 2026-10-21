# claude-scheduler

Claude Codeの5時間利用枠(rolling window)を狙ったタイミングで開始させるための
軽量ping登録ツールです。Node.jsなどのランタイムは不要で、OSごとの単一実行ファイル
として配布・実行できます。

## 前提条件

- 実行環境に `claude` コマンド (Claude Code CLI) がPATHに通っていること
- Linux/macOS: `cron` (`crontab`コマンド) が使えること
- Windows: `schtasks` (標準搭載)

## 配布ファイル

[Releases](https://github.com/ivis-kuroda/claude-scheduler/releases) ページから、OSに合ったファイルをダウンロードしてください。

| ファイル | 対象 |
|---|---|
| `claude-scheduler-linux-amd64` | Linux (x86_64) |
| `claude-scheduler-windows-amd64.exe` | Windows (x86_64) |
| `claude-scheduler-darwin-arm64` | macOS (Apple Silicon) |

## 使い方

### Linux / macOS

```bash
chmod +x claude-scheduler-linux-amd64
sudo mv claude-scheduler-linux-amd64 /usr/local/bin/claude-scheduler   # PATHに置くと楽

claude-scheduler add 07:00 12:00 17:00
claude-scheduler list
claude-scheduler remove 12:00     # 指定した時刻のジョブを削除
claude-scheduler remove all      # すべて削除
```

### Windows (PowerShell)

```powershell
# 適当なフォルダに置いてPATHを通す、もしくはフルパスで実行
.\claude-scheduler-windows-amd64.exe add 07:00 12:00 17:00
.\claude-scheduler-windows-amd64.exe list
.\claude-scheduler-windows-amd64.exe remove 12:00
.\claude-scheduler-windows-amd64.exe remove all
```

Windowsでは、以下の `claude-scheduler` を `.\claude-scheduler-windows-amd64.exe` に読み替えてください。

## コマンド

### `claude-scheduler add`

指定した時刻にpingを送るジョブを登録します。Linux/macOSではcrontabに、Windowsではタスクスケジューラに登録します。

同じ時刻のジョブがすでにあれば上書きし、それ以外の時刻のジョブは残します。

#### 使い方

```bash
claude-scheduler add <時刻...> [オプション]
```

#### 引数

| 引数 | |
|---|---|
| `<時刻...>` | pingを送る時刻。`HH:MM` 形式 (例: `07:00`, `7:05`)。スペース区切りで複数指定できる |

#### オプション

| オプション | |
|---|---|
| `--days <list>` | 実行する曜日。カンマ区切りで `sun`, `mon`, `tue`, `wed`, `thu`, `fri`, `sat` から指定する (デフォルト: `mon,tue,wed,thu,fri`) |
| `--prompt <text>` | 送信するプロンプト。`"` と改行は使えない (デフォルト: `ok`) |
| `--log <path>` | 実行結果を追記するログファイルのパス。`"` と改行は使えない (デフォルト: なし) |

#### 例

```bash
# 平日の7時・12時・17時に登録
claude-scheduler add 07:00 12:00 17:00

# 土日の9時に、プロンプトとログファイルを指定して登録
claude-scheduler add 09:00 --days sat,sun --prompt "hello" --log ~/claude-ping.log
```

### `claude-scheduler list`

このツールで登録したジョブの一覧を表示します。

#### 使い方

```bash
claude-scheduler list
```

### `claude-scheduler remove`

このツールで登録したジョブを削除します。このツール以外で登録したcronの行やタスクには影響しません。

#### 使い方

```bash
claude-scheduler remove <時刻...>
claude-scheduler remove all
```

#### 引数

| 引数 | |
|---|---|
| `<時刻...>` | 削除するジョブの時刻。`HH:MM` 形式。スペース区切りで複数指定できる。登録されていない時刻は警告を表示して無視する |
| `all` | このツールで登録したすべてのジョブを削除する。時刻と同時には指定できない |

#### 例

```bash
# 12時と17時のジョブを削除
claude-scheduler remove 12:00 17:00

# すべて削除
claude-scheduler remove all
```

### `claude-scheduler help`

ヘルプを表示します。`-h`, `--help` も同じです。

#### 使い方

```bash
claude-scheduler help
```

## 注意事項

- 各pingは実際にClaude Codeを1回呼び出すため、わずかに利用量を消費します。利用量を抑えるため、pingはhaikuモデルで送信します。
- 利用枠の起算タイミングやリセット周期の正確な仕様は変更される可能性があります。
  最新の挙動は公式ドキュメントで確認してください。
  https://code.claude.com/docs/ja/overview
- 本ツールはあくまで「早い時間に軽い呼び出しを送って枠を開始させる」という
  スケジューリングの補助であり、利用制限の仕組み自体を変更・回避するものではありません。
