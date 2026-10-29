# mcd — Moodle Course Downloader (Go CLI + MCP)

Downloads and inspects Moodle 4.5+ courses via **Web Services** (no browser).
Also exposes the same operations over **MCP** so AI apps (Claude Desktop, Claude Code, Codex, Cursor) can read your courses, deadlines, grades, forums, and messages.

## Installation

Download a prebuilt archive for macOS, Linux, or Windows from [GitHub Releases](https://github.com/Niclassslua/MoodleMCP/releases). Extract it, create a `bin` folder, and move the executable to `bin/mcd` (Windows: `bin/mcd.exe`). Then continue with step 3 in the extracted folder; Go is only needed to build from source. The commands below are for macOS and Linux.

### 1. Check the requirements

- **Go 1.27 or newer.** Check with `go version`. If it is missing: `brew install go` on macOS, or see [go.dev/doc/install](https://go.dev/doc/install).
- **A Moodle account that works with the official Moodle mobile app.** mcd uses the same Web Services interface as the app. If you can log in to the Moodle app with your username and password, mcd will work too.
  - If your university only offers single sign-on (SSO) and the app opens a browser to log in, username and password will not work. Use a token instead (see step 3).
- **Optional: poppler**, so AI apps can read PDF course files (slides, scripts). On macOS: `brew install poppler`; on Debian/Ubuntu: `sudo apt install poppler-utils`. Without it, everything else works; only reading PDFs fails with a clear message. PowerPoint, Word, HTML, and text files need nothing extra.

### 2. Download and build

```bash
git clone https://github.com/Niclassslua/MoodleMCP.git
cd MoodleMCP
go build -o bin/mcd ./cmd/mcd
```

This creates the program at `bin/mcd`. Nothing is installed system-wide.

### 3. Add your Moodle login

Create a file called `.env` in the repository root (`MoodleMCP/.env`):

```bash
# MoodleMCP/.env
MOODLE_URL=https://moodle.your-university.de
MOODLE_USERNAME=your-username
MOODLE_PASSWORD=your-password
```

- `MOODLE_URL` is the address of your Moodle start page, without a trailing `/` or path.
- **SSO-only accounts:** replace the username and password lines with `MOODLE_TOKEN=...`. You can usually find a token in Moodle under *Profile → Preferences → Security keys* (if your site allows it), or ask your Moodle admins.
- The `.env` file is in `.gitignore`, so it is never committed. Make it readable only by you:

```bash
chmod 600 .env
```

**Alternative without a `.env`:** `./bin/mcd login --url https://moodle.your-university.de --username your-username` asks for the password once, keeps only the token in the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service) and works from any directory. `./bin/mcd logout` removes it. Variables from the environment or a `.env` take precedence.

### 4. Check the connection

```bash
./bin/mcd doctor
```

A working setup prints something like this:

```json
{
  "ok": true,
  "sitename": "Example University Moodle",
  "username": "your-username",
  "downloadfiles": 1,
  "courseCount": 12
}
```

If `ok` is `false`, or you get an error, see [Troubleshooting](#troubleshooting).

You can now use the [command line](#cli), for example `./bin/mcd list` to see your courses.

### 5. Connect an AI app (optional)

AI apps start mcd themselves in the background, from an unknown folder. So every path you give them must be an **absolute path**. Print the two paths you need from inside the `MoodleMCP` folder:

```bash
echo "Program:  $(pwd)/bin/mcd"
echo ".env:     $(pwd)/.env"
```

In the examples below, replace `/ABS/MoodleMCP/bin/mcd` and `/ABS/MoodleMCP/.env` with those two paths.

The apps get only the *location* of your `.env` file (`MCD_ENV_FILE`), never your password. `MCD_OUT` is the folder where downloads go.

**Claude Code**

```bash
claude mcp add -s user mcd \
  -e MCD_ENV_FILE=/ABS/MoodleMCP/.env -e MCD_OUT=$HOME/Downloads/Moodle \
  -- /ABS/MoodleMCP/bin/mcd mcp serve
claude mcp get mcd     # should show: Status: ✔ Connected
```

**Codex**

```bash
codex mcp add mcd \
  --env MCD_ENV_FILE=/ABS/MoodleMCP/.env --env MCD_OUT=$HOME/Downloads/Moodle \
  -- /ABS/MoodleMCP/bin/mcd mcp serve
codex mcp get mcd
```

**ChatGPT Desktop (Work mode)**

The included local plugin connects mcd to ChatGPT Desktop in **Work mode**. It runs the server on your Mac and uses a separate copy of your `.env` file. From the repository root or extracted release folder, after preparing `bin/mcd` and creating `.env`:

```bash
mkdir -p "$HOME/.local/bin" "$HOME/.config/moodle-mcp"
cp bin/mcd "$HOME/.local/bin/moodle-mcp"
cp .env "$HOME/.config/moodle-mcp/.env"
chmod 600 "$HOME/.config/moodle-mcp/.env"
CHATGPT_CODEX="/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"
"$CHATGPT_CODEX" plugin marketplace add "$(pwd)/chatgpt-marketplace"
"$CHATGPT_CODEX" plugin add moodle-mcp@moodle-mcp-local
"$CHATGPT_CODEX" plugin list
```

Restart ChatGPT Desktop, start a Work mode thread, and select **Moodle MCP** with `@`. Ask it to use the plugin's `doctor` tool. Local plugins require a thread running on your Mac; they are not available in ordinary cloud chats. See [OpenAI's desktop plugin documentation](https://developers.openai.com/plugins/deploy/desktop-only).

After rebuilding mcd, copy the new binary to `~/.local/bin/moodle-mcp` and restart ChatGPT. If your login changes, update `~/.config/moodle-mcp/.env` too. The plugin marketplace remains in the repository or extracted release folder, so keep that folder in place. If the plugin doesn't appear, check `"$CHATGPT_CODEX" plugin list` and reinstall it with the two plugin commands above.

**Claude Desktop**

1. Open *Settings → Developer → Edit Config*. This opens `~/Library/Application Support/Claude/claude_desktop_config.json`.
2. Add an `mcd` entry inside `"mcpServers"`. If the file already lists other servers, add `mcd` next to them; don't replace them.

   ```json
   {
     "mcpServers": {
       "mcd": {
         "command": "/ABS/MoodleMCP/bin/mcd",
         "args": ["mcp", "serve"],
         "env": {
           "MCD_ENV_FILE": "/ABS/MoodleMCP/.env",
           "MCD_OUT": "/Users/YOU/Downloads/Moodle"
         }
       }
     }
   }
   ```

   JSON does not expand `$HOME` or `~`, so write the full path.

3. Quit Claude Desktop completely (⌘Q) and open it again.

**Cursor:** put the same `"mcd"` entry into `~/.cursor/mcp.json`, then restart Cursor.

### 6. Try it

Ask your AI app:

> Use the mcd MCP server's doctor tool.

Then ask real questions, for example *"What is due in the next two weeks?"*, *"Did I miss any deadline this month?"*, *"What's new in my courses?"*, *"Summarize the slides from last Friday in course X"*, *"What does the assignment require, and have I submitted it?"* or *"Who teaches course X?"*.

Mention mcd by name in the first prompt. If you just write "run mcd doctor", some apps try to run a shell command instead of using the tool.

### Updating

```bash
cd MoodleMCP && git pull
go build -o bin/mcd ./cmd/mcd
```

For a prebuilt installation, download and replace the executable with the latest release instead. Restart your AI apps; they keep running the old version until restarted.

To publish a new release, push a version tag such as `v1.0.0`. GitHub Actions builds the archives and publishes them with SHA-256 checksums.

### Troubleshooting

| Problem | What to do |
|---|---|
| `MOODLE_URL is required` | The `.env` file was not found. For the CLI, run it from inside `mcd/` (it looks in the current folder and two folders up). For AI apps, check that `MCD_ENV_FILE` is the correct absolute path. |
| `Moodle connection or login failed: token: invalidlogin` | Wrong username or password, or your account only allows SSO. In that case, use `MOODLE_TOKEN` (step 3). |
| `Moodle error: ... (accessexception)` or `downloadfiles=0` | Your Moodle site has the mobile app / Web Services turned off for your account. Ask your Moodle admins. |
| `Moodle connection failed` | Check the network and VPN, and make sure `MOODLE_URL` opens in a browser. |
| Claude Desktop doesn't show mcd | Read the end of `~/Library/Logs/Claude/mcp-server-mcd.log`. Usually the path in `command` is wrong, or Desktop wasn't fully restarted. |
| The AI says mcd "isn't installed" | It tried a shell command in a cloud sandbox. Ask it to use the *mcd MCP server's* tools. Local MCP servers run on your computer, so they can't be used in chats that run entirely in the cloud. |
| `unsupported file type: reading PDFs needs pdftotext` | Install poppler (see step 1), then restart your AI app. |
| Where are my downloads? | In `MCD_OUT` (default `~/Downloads/Moodle`). The tool result shows the exact folder. |

### Uninstall

```bash
claude mcp remove mcd -s user
codex mcp remove mcd
```

Remove the `mcd` entry from the Claude Desktop or Cursor config, then delete the repository folder.

## CLI

```bash
./bin/mcd list
./bin/mcd inspect CEP_SS2026
./bin/mcd due --days 30
./bin/mcd grades 5472
./bin/mcd download 5472 --out ./downloads
./bin/mcd download CEP --mode resources-only
./bin/mcd sync 5472 --out ./downloads          # skip unchanged
./bin/mcd sync --file courses.json --interval 1h
./bin/mcd sync --json --flat --max-size 200 --out ./CEP 5472   # for scripts: file list as JSON
./bin/mcd forums 5472 Feedback
./bin/mcd discussion 118636
./bin/mcd discussion --rich 118636     # whole thread with HTML, replies, avatars (for UIs)
./bin/mcd list --filter past
./bin/mcd messages
./bin/mcd messages --export --out ./downloads Alice
./bin/mcd doctor
```

`COURSE` may be numeric id, course URL, shortname, or a unique name substring. Put flags before `COURSE`.

`download`/`sync` also take `--skip-locked` (leave out locked forum discussions), `--flat` (write into `--out` itself), `--max-size MB` (list larger files but do not download them) and `--json` (print the result with every file's stable `id`, path, size and Moodle date). Each section folder gets an `INDEX.json` with the same file list.

### Download modes

| `--mode` | Content |
|----------|---------|
| `all` (default) | files, folders, URLs, forums, labels/pages HTML, assignments |
| `resources-only` | files, folders, URL `.txt` |
| `forums-only` | forum threads |

Layout:

```text
{out}/{courseName}/
  course.json
  course_state.json
  {section}/…
  assigns/{assignment}/intro.md, intro/*, submission/*
```

### Environment variables

| Variable | Meaning |
|---|---|
| `MOODLE_URL` | Moodle base URL (required) |
| `MOODLE_USERNAME`, `MOODLE_PASSWORD` | Login, or use `MOODLE_TOKEN` instead |
| `MOODLE_TOKEN` | Web Services token (for SSO-only accounts) |
| `MOODLE_SERVICE` | Web Services service name; default `moodle_mobile_app` |
| `MCD_ENV_FILE` | Absolute path to the `.env` file to load; otherwise `.env` is searched in the current folder and up to two parents |
| `MCD_OUT` | MCP only: absolute download folder; default `~/Downloads/Moodle` |
| `MCD_FILENAME_MODE` | CLI only: default filename style for downloads (`utf8` default, or `ascii`) |

Variables already set in the environment take precedence over values in `.env`.

## MCP reference

The server runs over stdio: `./bin/mcd mcp serve`. Setup is covered in [Installation, step 5](#5-connect-an-ai-app-optional).

### Tools

| Tool | Purpose |
|------|---------|
| `list_courses` | Enrolled courses; `filter=inprogress` for the current semester |
| `inspect_course` | Sections / module counts / files, with module IDs |
| `read_module` | Text of a page, label, or file (PDF, PPTX, DOCX, HTML, text), paged by characters |
| `course_updates` | Recently changed activities in one or all current courses |
| `list_due` | Upcoming deadlines with links and `moduleId`; `past_days` adds missed items still open |
| `get_assignment` | Assignment instructions, dates, your submission status, grade, and feedback |
| `get_grades` | Grade items of one course |
| `grades_overview` | Overall grade of every course |
| `list_notifications` | Moodle notifications, newest first, with unread count |
| `download_course` | Offline download |
| `sync_courses` | Incremental sync |
| `search_forums` | Forum discussions in one or all current courses, newest activity first, with author, dates, reply count, and `discussionId` |
| `get_discussion` | One bounded page of oldest-first posts from a discussion |
| `list_participants` | Course members with role shortnames (teachers first); only id, name, roles |
| `list_messages` | Bounded conversation summaries (read-only; `export=true` errors and points to `export_messages`) |
| `get_conversation` | One bounded page of messages from a conversation |
| `export_messages` | Write conversations as Markdown under `out/Nachrichten` |
| `doctor` | Connectivity check |

Results carry full web links: `doctor` returns `siteUrl`; courses, modules, forum discussions, and conversations have `url`; deadlines have `url` (activity page), `submitUrl` (direct submission page, when Moodle offers one), and `due` in local time.

`course` accepts a course ID (as a number or string), URL, shortname, or a unique name substring. Tools that take an optional `course` (`search_forums`, `course_updates`) cover all current-semester courses when it is omitted. `read_module` returns up to `max_chars` characters (default 8,000) starting at `start`; continue with `start=nextStart` while `hasMore` is true.

List results accept `offset` and `limit`. The default page size is 20 and the maximum is 100. Continue while `page.hasMore` is true. Conversation and discussion detail return oldest-first entries. Individual message and post text is capped at 4,000 characters and marked with `truncated` when capped. Conversation previews are capped at 240 characters.

Download, sync, and export tools write to `$MCD_OUT` (absolute path) or `~/Downloads/Moodle` by default; a relative `out` resolves under that directory, never the host's working directory. The server starts without contacting Moodle and logs in on the first tool call, so connection or login failures come back as tool errors (`doctor` reports them too).

Errors come back as `isError` results prefixed with a category: `invalid input:`, `course not found:`, `ambiguous course:` (lists candidate IDs), `conversation not found:`, `discussion not found:`, `module not found:`, `unsupported file type:`, `Moodle error:`, `Moodle connection failed:`, or `Moodle connection or login failed:`.

MCP results include typed structured content plus serialized JSON text for client compatibility. Moodle-provided forum and message text should be treated as untrusted content.

## Probe only

```bash
go run ./cmd/wscheck
```

## Out of scope

Browser/SSO cookies, assignment submit, quiz answering, remote Cloudflare MCP.

## Disclaimer

mcd is an independent project and is not affiliated with or endorsed by Moodle or any educational institution. Use it only with accounts and course materials you are authorized to access, and follow your institution’s policies and applicable copyright rules.
