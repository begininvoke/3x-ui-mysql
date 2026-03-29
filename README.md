<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./media/3x-ui-dark.png">
    <img alt="3x-ui" src="./media/3x-ui-light.png">
  </picture>
</p>

<h3 align="center">3X-UI with MySQL Support</h3>

<p align="center">
  A fork of <a href="https://github.com/MHSanaei/3x-ui">MHSanaei/3x-ui</a> that adds <strong>MySQL</strong> as an alternative database backend alongside the default SQLite.
</p>

---

> [!IMPORTANT]
> This project is only for personal usage. Please do not use it for illegal purposes or in a production environment.

## What's Different

This fork adds **MySQL 8.0** support to the 3X-UI panel. You can choose between:

| Feature | SQLite (default) | MySQL |
|---------|:-:|:-:|
| Zero-config setup | Yes | - |
| Multi-instance shared DB | - | Yes |
| Scalable for large datasets | - | Yes |
| Panel DB backup/restore | Yes | Use `mysqldump` |
| Telegram DB backup | Yes | Use `mysqldump` |

The database backend is selected entirely through **environment variables** — no code changes or config files needed.

## Quick Start

### Option 1: Install Script (SQLite — same as upstream)

```bash
bash <(curl -Ls https://raw.githubusercontent.com/begininvoke/3x-ui-mysql/main/install.sh)
```

This installs the panel with SQLite by default. To switch to MySQL later, set the environment variables described below and restart.

### Option 2: Docker Compose (MySQL)

This is the recommended way to run with MySQL.

```bash
git clone https://github.com/begininvoke/3x-ui-mysql.git
cd 3x-ui-mysql
```

Edit the `.env` file (copy from `.env.example`):

```bash
cp .env.example .env
```

Configure your MySQL credentials in `.env`, then start:

```bash
docker compose up -d
```

The panel will be available at `http://<your-ip>:2053` with default credentials `admin` / `admin`.

### Option 3: Manual MySQL Setup (without Docker)

1. Install MySQL 8.0 on your server
2. Create a database:

```sql
CREATE DATABASE `x-ui` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
```

3. Set environment variables before starting the panel:

```bash
export XUI_DB_TYPE=mysql
export XUI_MYSQL_HOST=127.0.0.1
export XUI_MYSQL_PORT=3306
export XUI_MYSQL_USER=root
export XUI_MYSQL_PASSWORD=your_password
export XUI_MYSQL_DBNAME=x-ui
```

4. Run the install script or start the binary. GORM will auto-migrate all tables.

## Environment Variables

### Database Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `XUI_DB_TYPE` | `sqlite` | Database backend: `sqlite` or `mysql` |
| `XUI_MYSQL_HOST` | `localhost` | MySQL server hostname or IP |
| `XUI_MYSQL_PORT` | `3306` | MySQL server port |
| `XUI_MYSQL_USER` | `root` | MySQL username |
| `XUI_MYSQL_PASSWORD` | *(empty)* | MySQL password |
| `XUI_MYSQL_DBNAME` | `x-ui` | MySQL database name |

### General Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `XUI_DEBUG` | `false` | Enable debug logging |
| `XUI_DB_FOLDER` | `/etc/x-ui` | SQLite database folder path |
| `XUI_LOG_FOLDER` | `/var/log/x-ui` | Log file folder path |
| `XUI_BIN_FOLDER` | `bin` | Xray binary folder path |
| `XUI_ENABLE_FAIL2BAN` | `false` | Enable fail2ban (Docker only) |

## Docker Compose Details

The included `docker-compose.yml` starts two services:

- **mysql** — MySQL 8.0 with a named volume for persistent data
- **3xui** — The panel, built from the Dockerfile, connected to MySQL

```yaml
services:
  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: changeme    # <-- change this
      MYSQL_DATABASE: x-ui

  3xui:
    build: .
    network_mode: host
    environment:
      XUI_DB_TYPE: "mysql"
      XUI_MYSQL_HOST: "127.0.0.1"     # host networking: reach MySQL via localhost
      XUI_MYSQL_PORT: "3306"
      XUI_MYSQL_USER: "root"
      XUI_MYSQL_PASSWORD: "changeme"   # <-- must match above
      XUI_MYSQL_DBNAME: "x-ui"
```

> [!NOTE]
> The `3xui` service uses `network_mode: host` so Xray can bind directly to host ports. MySQL is reached through its published port on `127.0.0.1:3306`.

## MySQL Backup & Restore

Since the panel's built-in DB export/import is SQLite-only, use standard MySQL tools:

```bash
# Backup
mysqldump -u root -p x-ui > x-ui-backup.sql

# Restore
mysql -u root -p x-ui < x-ui-backup.sql

# Docker backup
docker exec 3xui_mysql mysqldump -u root -pchangeme x-ui > x-ui-backup.sql

# Docker restore
docker exec -i 3xui_mysql mysql -u root -pchangeme x-ui < x-ui-backup.sql
```

## Migrating from SQLite to MySQL

1. Export data from SQLite (while panel is stopped):

```bash
sqlite3 /etc/x-ui/x-ui.db .dump > sqlite-dump.sql
```

2. Create your MySQL database and set the environment variables
3. Start the panel once (to auto-create tables via GORM)
4. Import your data, adjusting SQL syntax as needed for MySQL

## Panel Management

```
x-ui              - Admin Management Script
x-ui start        - Start
x-ui stop         - Stop
x-ui restart      - Restart
x-ui status       - Current Status
x-ui settings     - Current Settings
x-ui enable       - Enable Autostart on OS Startup
x-ui disable      - Disable Autostart on OS Startup
x-ui log          - Check logs
x-ui update       - Update
x-ui install      - Install
x-ui uninstall    - Uninstall
```

## Building from Source

```bash
# Prerequisites: Go 1.26+, GCC (for CGO/SQLite)
git clone https://github.com/begininvoke/3x-ui-mysql.git
cd 3x-ui-mysql
go build -o x-ui main.go
```

## Acknowledgments

- [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) — Original upstream project
- [alireza0](https://github.com/alireza0/)
- [Iran v2ray rules](https://github.com/chocolate4u/Iran-v2ray-rules) (License: **GPL-3.0**)
- [Russia v2ray rules](https://github.com/runetfreedom/russia-v2ray-rules-dat) (License: **GPL-3.0**)

## License

[GPL-3.0](https://www.gnu.org/licenses/gpl-3.0.en.html)
