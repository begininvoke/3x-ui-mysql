[English](/README.md) | [فارسی](/README.fa_IR.md) | [العربية](/README.ar_EG.md) | [中文](/README.zh_CN.md) | [Español](/README.es_ES.md) | [Русский](/README.ru_RU.md)

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./media/3x-ui-dark.png">
    <img alt="3x-ui" src="./media/3x-ui-light.png">
  </picture>
</p>

# 3X-UI با پشتیبانی از MySQL

**3X-UI** یک پنل کنترل پیشرفته، مبتنی بر وب و متن‌باز (Open Source) است که برای مدیریت سرور **Xray-core** طراحی شده. این پنل یک رابط کاربری گرافیکی ساده و قدرتمند برای پیکربندی، نظارت و مدیریت پروتکل‌های مختلف VPN و پروکسی ارائه می‌دهد.

> این نسخه (Fork) توسط `begininvoke` توسعه داده شده و قابلیت استفاده از **MySQL 8.0** را به عنوان پایگاه داده جایگزین به پنل اصلی اضافه کرده است.

---

## 🧠 این پروژه دقیقاً چه کاری انجام می‌دهد؟

تصور کنید یک سرور خارجی دارید و می‌خواهید روی آن یک سرویس پروکسی (مثل Xray) راه‌اندازی کنید. این پنل:

1. **Xray-core** را مدیریت می‌کند — راه‌اندازی، نگهداری و ری‌استارت خودکار
2. **اینباند** (Inbound) تعریف می‌کند — پورت‌ها و پروتکل‌های ورودی مثل VMESS، VLESS، Trojan، Shadowsocks و...
3. **کلاینت** (Client) می‌سازد — لینک‌های اختصاصی برای هر کاربر با حجم و زمان محدود
4. **ترافیک** را مانیتور می‌کند — مصرف هر کاربر به تفکیک Up/Down
5. **اشتراک (Subscription)** ارائه می‌دهد — لینک ساب‌اسکریپشن برای import خودکار در کلاینت‌ها
6. **بات تلگرام** دارد — گزارش روزانه، هشدار مصرف، و مدیریت از طریق ربات
7. **Fail2ban** دارد — محافظت در برابر حملات Brute-force به پنل

---

## 🔄 تفاوت این Fork با نسخه اصلی

| ویژگی | نسخه اصلی (SQLite) | این Fork (MySQL) |
|--------|:-----:|:-------:|
| راه‌اندازی بدون تنظیمات | ✅ | نیاز به MySQL |
| اشتراک دیتابیس بین چند سرور | ❌ | ✅ |
| مقیاس‌پذیری برای حجم داده بالا | محدود | ✅ |
| Backup/Restore از طریق پنل | ✅ | با `mysqldump` |
| Backup تلگرام | ✅ | با `mysqldump` |

**مهم:** نوع دیتابیس فقط از طریق **متغیر محیطی** انتخاب می‌شود — بدون نیاز به تغییر کد!

---

## 🏗 معماری داخلی (Internal Architecture)

```
┌─────────────────────────────────────────────────────┐
│                  3X-UI Panel                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │ Web UI   │  │ Sub API  │  │ Telegram Bot     │  │
│  │ (Gin)    │  │ (Gin)    │  │ (telego)         │  │
│  └────┬─────┘  └────┬─────┘  └───────┬──────────┘  │
│       │             │                │             │
│  ┌────▼─────────────▼────────────────▼──────────┐   │
│  │          Service Layer                       │   │
│  │  (UserService, InboundService, ...)          │   │
│  └────────────────┬─────────────────────────────┘   │
│                   │                                 │
│  ┌────────────────▼─────────────────────────────┐   │
│  │       GORM Database Layer                    │   │
│  │  ┌──────────┐    ┌──────────────────────┐    │   │
│  │  │ SQLite   │ OR │  MySQL 8.0           │    │   │
│  │  └──────────┘    └──────────────────────┘    │   │
│  └──────────────────────────────────────────────┘   │
│                                                     │
│  ┌──────────────────────────────────────────────┐   │
│  │           Xray-core (Go)                     │   │
│  │  ┌──────┐ ┌──────┐ ┌────────────────────┐   │   │
│  │  │Vmess │ │Vless │ │Trojan/Shadowsocks  │   │   │
│  │  └──────┘ └──────┘ └────────────────────┘   │   │
│  └──────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

---

## 📂 اجزای اصلی کد (Code Structure)

### 1️⃣ `main.go` — نقطه ورود
فایل اصلی برنامه که:
- متغیرهای محیطی را بارگذاری می‌کند (با `godotenv`)
- دیتابیس را مقداردهی اولیه می‌کند
- وب سرور (Gin) و Sub Server را راه می‌اندازد
- سیگنال‌های سیستمی (SIGHUP برای restart, SIGTERM برای خاموشی) را مدیریت می‌کند

### 2️⃣ `config/config.go` — تنظیمات
مدیریت تمام تنظیمات از طریق متغیرهای محیطی:
- `XUI_DB_TYPE`: نوع دیتابیس (`sqlite` یا `mysql`)
- `XUI_MYSQL_HOST`, `XUI_MYSQL_PORT`, ... : اتصال MySQL
- `XUI_DEBUG`: لاگ دیباگ
- `XUI_DB_FOLDER`, `XUI_LOG_FOLDER`: مسیرهای فایل

### 3️⃣ `database/db.go` — لایه دیتابیس (GORM)

نحوه کار دیتابیس:

```go
func InitDB(dbPath string) error {
    if config.IsMySQL() {
        dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&...",
            user, pass, host, port, dbname)
        db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{...})
    } else {
        db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{...})
    }
}
```

مدل‌های دیتابیس شامل:
- **User**: کاربران پنل (admin)
- **Inbound**: کانفیگ‌های اینباند با ترافیک، تنظیمات و StreamSettings
- **ClientTraffic**: ترافیک هر کلاینت
- **Setting**: تنظیمات پنل (پورت، SSL، مسیرها و...)
- **TrafficDaily**: آمار روزانه ترافیک
- **PanelRestart**: تاریخچه ری‌استارت‌ها
- **BlockedIP**: آی‌پی‌های مسدود شده
- **NetworkInsightsSnapshot**: عکس‌های لحظه‌ای از وضعیت شبکه

### 4️⃣ `database/model/model.go` — مدل‌های داده

پروتکل‌های پشتیبانی‌شده:
- `VMESS` — پروتکل اصلی V2Ray
- `VLESS` — نسخه سبک‌تر VMESS
- `Trojan` — پروتکل امن HTTPS
- `Shadowsocks` — پروکسی سبک
- `HTTP` — پروکسی HTTP ساده
- `Mixed` — ترکیبی از چند پروتکل
- `WireGuard` — VPN مدرن
- `Tunnel` — تونل‌سازی

### 5️⃣ `sub/sub.go` — سرویس اشتراک (Subscription)

یک سرور HTTP/HTTPS جداگانه که:
- لینک ساب‌اسکریپشن برای کلاینت‌ها ارائه می‌دهد
- پشتیبانی از JSON subscription
- قابلیت رمزنگاری لینک‌ها
- پشتیبانی از Fragment، Mux و Noise برای دور زدن فیلترینگ
- امکان تعریف قوانین Routing در کانفیگ خروجی

### 6️⃣ `Dockerfile`

ساخت دو مرحله‌ای (Multi-stage build):
1. **Builder**: کامپایل Go و دانلود Xray-core
2. **Final**: Alpine لینوکس + fail2ban + کانفیگ نهایی

پورت پیش‌فرض: `2053`

### 7️⃣ `docker-compose.yml`

دو سرویس:
- **mysql**: کانتینر MySQL 8.0 با volume مجزا
- **3xui**: برنامه اصلی که منتظر می‌ماند تا MySQL آماده شود (`depends_on: condition: service_healthy`)

---

## 🚀 روش‌های استقرار (Deployment)

### روش ۱: نصب مستقیم (SQLite)
```bash
bash <(curl -Ls https://raw.githubusercontent.com/begininvoke/3x-ui-mysql/main/install.sh)
```

### روش ۲: Docker Compose (پیشنهادی برای MySQL)
```bash
git clone https://github.com/begininvoke/3x-ui-mysql.git
cd 3x-ui-mysql
cp .env.example .env
# ویرایش .env با رمز MySQL دلخواه
docker compose up -d
```

> پنل روی `http://your-ip:2053` با نام کاربری `admin` و رمز `admin` در دسترس خواهد بود.

### روش ۳: MySQL دستی (بدون Docker)
```bash
# ابتدا دیتابیس MySQL را بسازید
mysql -u root -p -e "CREATE DATABASE \`x-ui\` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;"

# سپس متغیرهای محیطی را ست کنید
export XUI_DB_TYPE=mysql
export XUI_MYSQL_HOST=127.0.0.1
export XUI_MYSQL_PORT=3306
export XUI_MYSQL_USER=root
export XUI_MYSQL_PASSWORD=your_password
export XUI_MYSQL_DBNAME=x-ui

# نصب را اجرا کنید
bash <(curl -Ls https://raw.githubusercontent.com/begininvoke/3x-ui-mysql/main/install.sh)
```

---

## 🛠 نحوه کار قدم به قدم

### ۱. تعریف اینباند
از طریق پنل وب (پورت ۲۰۵۳) یک اینباند جدید تعریف می‌کنید:
- پروتکل (VMESS, VLESS, Trojan, ...)
- پورت
- Stream Settings (TLS, WS, gRPC, Reality, ...)
- محدودیت حجم و زمان

### ۲. اضافه کردن کلاینت
برای هر اینباند می‌توانید چندین کلاینت با حجم و زمان مجزا تعریف کنید.

### ۳. اتصال کلاینت‌ها
دو روش:
- **لینک مستقیم**: کپی لینک و افزودن به کلاینت (مثل v2rayNG, Streisand, V2Box, ...)
- **Subscription**: لینک اشتراک که کلاینت به صورت دوره‌ای از آن آپدیت می‌گیرد

### ۴. مانیتورینگ
پنل آمار زنده نشان می‌دهد:
- ترافیک لحظه‌ای هر کاربر
- مصرف روزانه/ماهانه
- آنلاین بودن سرور
- هشدار از طریق تلگرام

---

## ⚙️ متغیرهای محیطی مهم

### دیتابیس
| متغیر | پیش‌فرض | توضیح |
|--------|---------|-------|
| `XUI_DB_TYPE` | `sqlite` | `sqlite` یا `mysql` |
| `XUI_MYSQL_HOST` | `localhost` | آدرس MySQL |
| `XUI_MYSQL_PORT` | `3306` | پورت MySQL |
| `XUI_MYSQL_USER` | `root` | نام کاربری |
| `XUI_MYSQL_PASSWORD` | — | رمز عبور |
| `XUI_MYSQL_DBNAME` | `x-ui` | نام دیتابیس |

### عمومی
| متغیر | پیش‌فرض | توضیح |
|--------|---------|-------|
| `XUI_DEBUG` | `false` | لاگ دیباگ |
| `XUI_DB_FOLDER` | `/etc/x-ui` | مسیر فایل SQLite |
| `XUI_LOG_FOLDER` | `/var/log/x-ui` | مسیر لاگ‌ها |
| `XUI_ENABLE_FAIL2BAN` | `false` | فعال‌سازی fail2ban |

---

## 🔧 Troubleshooting رایج

| مشکل | راه‌حل |
|-------|--------|
| `SSL_ERROR_SYSCALL` | اینترنت سرور را چک کنید یا از پروکسی استفاده کنید |
| `Access denied for user` | رمز MySQL در `.env` را بررسی کنید |
| پنل باز نمی‌شود | فایروال پورت ۲۰۵۳ را چک کنید |
| خطای `AutoMigrate` | دسترسی کاربر MySQL به CREATE TABLE را بررسی کنید |
| Xray کرش می‌کند | لاگ‌های `/var/log/x-ui` را بررسی کنید |
| Telegram Bot 409 Conflict | پنل restart می‌شود و بات به‌طور خودکار Stop می‌شود |

---

## 📜 لایسنس

این پروژه تحت لایسنس **GPL-3.0** منتشر شده است.

> ⚠️ **توجه:** این پروژه فقط برای استفاده شخصی و ارتباطات امن است. از آن برای اهداف غیرقانونی استفاده نکنید.

---

## 🤝 سپاس
- [MHSanaei](https://github.com/MHSanaei/3x-ui) — سازنده پروژه اصلی
- [alireza0](https://github.com/alireza0/) — همکاری در توسعه

## ⭐ پشتیبانی

اگر این پروژه برای شما مفید است، به آن یک ستاره ⭐ بدهید.