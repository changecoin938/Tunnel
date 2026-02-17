# paqet - تانل سطح پکت

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

`paqet` یک تانل دوطرفه در سطح پکت هست که با raw socket نوشته شده. ترافیک رو از سرور داخل (کلاینت) به سرور خارج (سرور) منتقل می‌کنه و از اونجا به مقصد وصل میشه. با کار کردن در سطح پکت، کاملاً از TCP/IP stack سیستم‌عامل عبور می‌کنه و برای انتقال امن از KCP استفاده می‌کنه.

```
[کاربر] <--> [Xray/V2ray] <--> [paqet کلاینت (داخل)] <=== Raw TCP ===> [paqet سرور (خارج)] <--> [Xray inbound] <--> [اینترنت]
```

---

## فهرست

- [نصب سریع](#نصب-سریع)
- [معماری و پورت‌ها](#معماری-و-پورتها)
- [ستاپ سرور خارج](#ستاپ-سرور-خارج)
- [ستاپ سرور داخل](#ستاپ-سرور-داخل)
- [تنظیمات Xray روی سرور خارج](#تنظیمات-xray-روی-سرور-خارج)
- [تنظیمات کلاینت (V2rayNG / Nekobox / ...)](#تنظیمات-کلاینت)
- [آپدیت paqet](#آپدیت-paqet)
- [منوی paqet-ui](#منوی-paqet-ui)
- [عیب‌یابی](#عیبیابی)
- [مرجع تنظیمات](#مرجع-تنظیمات)

---

## نصب سریع

روی **هر دو سرور** (داخل و خارج) این دستور رو بزنید:

```bash
curl -fsSL https://raw.githubusercontent.com/changecoin938/Tunnel/main/install.sh | sudo bash
```

بعد از نصب، منوی `paqet-ui` باز میشه.

> اگه بعداً خواستید دوباره منو رو باز کنید: `sudo paqet-ui`

---

## معماری و پورت‌ها

### نقشه کلی

```
سرور داخل (ایران)                          سرور خارج
┌─────────────────────┐                    ┌─────────────────────┐
│                     │                    │                     │
│  کاربران ──► :443   │   KCP Tunnel       │  :9999 ◄── paqet   │
│  (paqet forward)    │ ==================>│  (listen)           │
│                     │  10 کانکشن KCP     │                     │
│  paqet client       │                    │  paqet server       │
│  raw port: 20000    │                    │                     │
│                     │                    │  127.0.0.1:2443     │
│  127.0.0.1:5555 ──► │ ──── tunnel ────►  │  ──► Xray inbound   │
│  (management)       │                    │      (VLESS/TCP)    │
└─────────────────────┘                    └─────────────────────┘
```

### پورت‌ها

| پورت | کجا | چیه | توضیح |
|------|-----|-----|-------|
| `9999` | سرور خارج | پورت تانل KCP | پورت اصلی ارتباط بین دو سرور. باید در فایروال باز باشه |
| `9999-10008` | سرور خارج | رنج پورت KCP | با `conn=10`، پورت‌های 9999 تا 10008 استفاده میشن (10 تا) |
| `443` | سرور داخل | پورت ورودی کاربران | کاربران با V2rayNG/Nekobox به این پورت وصل میشن |
| `2443` | سرور خارج | Xray inbound | فقط روی `127.0.0.1` گوش میده. paqet ترافیک رو اینجا فوروارد می‌کنه |
| `20000-20009` | سرور داخل | raw port محلی | پورت‌های داخلی paqet برای ساخت پکت خام (10 تا برای 10 کانکشن) |
| `5555` | هر دو | management forward | پورت مدیریتی (اختیاری) |
| `6060` | هر دو | debug/pprof | فقط `127.0.0.1` - برای مانیتور و دیباگ |

### چند تا کانکشن KCP داریم؟

پیش‌فرض `conn=10` هست. یعنی:
- **10 کانکشن KCP موازی** بین داخل و خارج
- هر کانکشن روی یه پورت جداگانه (مثلاً 9999 تا 10008)
- هر کانکشن تا **4096 استریم** می‌تونه داشته باشه
- مجموع استریم‌ها: **65536**
- مجموع session‌ها: **2048**

با gRPC هر کاربر معمولاً 3-8 استریم مصرف می‌کنه. پس با تنظیمات پیش‌فرض حدود **200-500 کاربر همزمان** ساپورت میشه.

---

## ستاپ سرور خارج

### مرحله 1: نصب و اجرای UI

```bash
curl -fsSL https://raw.githubusercontent.com/changecoin938/Tunnel/main/install.sh | sudo bash
```

### مرحله 2: انتخاب "ستاپ سرور خارج"

از منو گزینه **1** رو بزنید:

```
1) ستاپ سرور خارج (Server)
```

اسکریپت خودش:
- اینترفیس، IP و MAC روتر رو پیدا می‌کنه
- پورت 9999 رو چک می‌کنه (اگه مشغوله راهنمایی میده)
- یه **کلید Pairing** تولید می‌کنه
- کانفیگ رو میسازه و سرویس رو استارت می‌کنه

### مرحله 3: کلید رو یادداشت کنید

بعد از ستاپ، یه کلید مثل این نشون داده میشه:

```
کلید Pairing (این را به سرور داخل بده):
xK9mP2qR7vB4nL8wT1yU6cF3hJ5gD0sA
```

**این کلید رو کپی کنید** - باید همین رو روی سرور داخل وارد کنید.

### مرحله 4: فایروال سرور خارج

> اگه از `paqet-ui` نصب کردید، قوانین iptables **خودکار** اعمال شده. ولی فایروال ابری (Security Group) رو باید دستی باز کنید.

در پنل ابری (Hetzner/OVH/DigitalOcean/AWS):

```
Protocol: TCP
Port Range: 9999-10008
Source: 0.0.0.0/0 (یا IP سرور داخل)
```

---

## ستاپ سرور داخل

### مرحله 1: نصب و اجرای UI

```bash
curl -fsSL https://raw.githubusercontent.com/changecoin938/Tunnel/main/install.sh | sudo bash
```

### مرحله 2: انتخاب "Setup inside (X-UI only)"

از منو گزینه **15** رو بزنید:

```
15) Setup inside (X-UI only, one-step, no SOCKS)
```

### مرحله 3: وارد کردن اطلاعات

اسکریپت سوال می‌کنه:

1. **IP سرور خارج؟** → IP سرور خارجتون رو بزنید (مثلاً `1.2.3.4`)
2. **کلید Pairing؟** → همون کلیدی که سرور خارج نشون داد رو پیست کنید
3. **پورت ورودی کاربران؟** → پیش‌فرض `443` (اگه مشغوله، پیشنهاد جایگزین میده)

### بعد از ستاپ

سرور داخل اینطوری کار می‌کنه:

```
کاربران ──► 0.0.0.0:443 ──► [paqet tunnel] ──► 127.0.0.1:2443 (Xray سرور خارج)
```

---

## تنظیمات Xray روی سرور خارج

بعد از ستاپ paqet، باید روی سرور خارج یه **Xray inbound** بسازید که روی `127.0.0.1:2443` گوش بده.

### با X-UI / 3X-UI

1. وارد پنل X-UI بشید
2. **Add Inbound** بزنید
3. تنظیمات:

| فیلد | مقدار |
|------|-------|
| **Remark** | `paqet-tunnel` (یا هرچی دوست دارید) |
| **Protocol** | `vless` |
| **Listen IP** | `127.0.0.1` |
| **Port** | `2443` |
| **Network** | `tcp` |
| **Security** | `none` |
| **Transmission** | `tcp` (یا `grpc`) |

> **خیلی مهم:** Listen IP حتماً `127.0.0.1` باشه، نه `0.0.0.0`. چون ترافیک از تانل paqet میاد و نباید مستقیم از اینترنت قابل دسترسی باشه.

### با X-UI و gRPC

اگه می‌خواید از gRPC استفاده کنید:

| فیلد | مقدار |
|------|-------|
| **Protocol** | `vless` |
| **Listen IP** | `127.0.0.1` |
| **Port** | `2443` |
| **Network** | `grpc` |
| **serviceName** | `paqet` (یا هرچی) |
| **Security** | `none` |

### بدون پنل (دستی)

اگه X-UI ندارید، این رو به `config.json` ایکس‌ری اضافه کنید:

```json
{
  "inbounds": [
    {
      "listen": "127.0.0.1",
      "port": 2443,
      "protocol": "vless",
      "settings": {
        "clients": [
          {
            "id": "YOUR-UUID-HERE"
          }
        ],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "tcp"
      }
    }
  ]
}
```

UUID بسازید: `xray uuid` یا آنلاین از سایت‌های UUID generator.

---

## تنظیمات کلاینت

### V2rayNG (اندروید)

1. **Add** → **VLESS**
2. تنظیمات:

| فیلد | مقدار |
|------|-------|
| **Address** | `IP سرور داخل (ایران)` |
| **Port** | `443` |
| **UUID** | همون UUID که در Xray ساختید |
| **Network** | `tcp` |
| **Security** | `none` |

### Nekobox / Clash

```yaml
- name: "paqet"
  type: vless
  server: IP_سرور_داخل
  port: 443
  uuid: YOUR-UUID-HERE
  network: tcp
  tls: false
```

### اگه از gRPC استفاده کردید

```yaml
- name: "paqet-grpc"
  type: vless
  server: IP_سرور_داخل
  port: 443
  uuid: YOUR-UUID-HERE
  network: grpc
  grpc-opts:
    grpc-service-name: "paqet"
  tls: false
```

> **توجه:** TLS روی `none` باشه چون رمزنگاری توسط KCP در سطح تانل انجام میشه.

---

## آپدیت paqet

### روش 1: آپدیت سریع (پیشنهادی)

روی **هر دو سرور** بزنید:

```bash
curl -fsSL https://raw.githubusercontent.com/changecoin938/Tunnel/main/update.sh | sudo bash
```

این دستور:
- آخرین نسخه رو از سورس بیلد می‌کنه
- باینری قبلی رو بکاپ می‌گیره
- جایگزین می‌کنه
- سرویس رو ریستارت می‌کنه
- اسکریپت‌های کمکی (paqet-ui و ...) رو هم آپدیت می‌کنه

**کانفیگ و کلید دست نمی‌خوره.**

### روش 2: آپدیت از یه برنچ یا کامیت خاص

```bash
# از برنچ main
curl -fsSL https://raw.githubusercontent.com/changecoin938/Tunnel/main/update.sh | sudo PAQET_SOURCE_REF=main bash

# از یه تگ خاص
curl -fsSL https://raw.githubusercontent.com/changecoin938/Tunnel/main/update.sh | sudo PAQET_SOURCE_REF=v1.0.1 bash
```

### بعد از آپدیت

```bash
paqet version                      # چک ورژن
sudo systemctl status paqet        # چک وضعیت سرویس
```

---

## منوی paqet-ui

با `sudo paqet-ui` منوی مدیریت باز میشه:

| گزینه | عملکرد |
|-------|--------|
| **1** | ستاپ سرور خارج (Server) |
| **15** | ستاپ سرور داخل (X-UI only) |
| **3** | مشاهده وضعیت سرویس |
| **4** | منوی دیباگ و عیب‌یابی |
| **6** | توقف سرویس |
| **7** | حذف (با حفظ باینری) |
| **13** | حذف کامل |

### منوی دیباگ (گزینه 4)

| گزینه | عملکرد |
|-------|--------|
| **1** | ساخت گزارش پشتیبانی (Support Report) |
| **2** | نمایش کانفیگ (کلید مخفی شده) |
| **3** | نمایش لاگ (200 خط آخر) |
| **4** | دنبال کردن لاگ زنده (Ctrl+C) |
| **5** | تنظیم سطح لاگ (info/debug/warn) |
| **6** | فعال‌سازی debug endpoints (pprof+diag) |
| **7** | غیرفعال‌سازی debug endpoints |
| **8** | نمایش وضعیت تانل (متنی) |
| **10** | مانیتور زنده (1 ثانیه‌ای) |
| **11** | تنظیم DSCP |
| **12** | بازتنظیم KCP (fast3 + بافرهای پیشنهادی) |

---

## عیب‌یابی

### تانل وصل نمیشه

1. **فایروال ابری:** پورت `9999-10008` TCP روی سرور خارج باز باشه
2. **کلید یکسان:** کلید Pairing باید روی هر دو سرور یکی باشه
3. **تست پینگ:** روی سرور داخل: `sudo paqet ping -c /etc/paqet/config.yaml`
4. **لاگ‌ها:** `sudo journalctl -u paqet -f`

### سرعت پایینه

1. `sudo paqet-ui` → گزینه 4 → گزینه 10 (مانیتور زنده)
2. ستون `app(up/down)` رو نگاه کنید
3. اگه `guard_drops` زیاده = ترافیک ناخواسته/حمله
4. اگه CPU بالاس = کانکشن‌ها رو کم کنید یا سرور بزرگ‌تر بگیرید

### OOM / کرش سرویس

1. بافرها رو کم کنید: `sudo paqet-ui` → گزینه 4 → گزینه 12 (Retune KCP)
2. لاگ کرنل: `dmesg | grep -i oom`
3. گزارش کامل: `sudo paqet-ui` → گزینه 4 → گزینه 1 (Support Report)

### سرویس استارت نمیشه

```bash
sudo systemctl status paqet         # ببینید چه اروری هست
sudo journalctl -u paqet -n 50      # 50 خط آخر لاگ
```

---

## مرجع تنظیمات

### تنظیمات اصلی

| پارامتر | پیش‌فرض | توضیح |
|---------|---------|-------|
| `role` | - | `"server"` یا `"client"` (اجباری) |
| `transport.protocol` | `"kcp"` | پروتکل انتقال |
| `transport.conn` | `10` | تعداد کانکشن‌های KCP موازی |
| `transport.kcp.mode` | `"fast3"` | حالت KCP (سریع‌ترین) |
| `transport.kcp.key` | - | کلید رمزنگاری (باید یکسان باشه) |
| `transport.kcp.block` | `"aes-128-gcm"` | الگوریتم رمزنگاری |

### بافرها و محدودیت‌ها

| پارامتر | پیش‌فرض | توضیح |
|---------|---------|-------|
| `transport.kcp.rcvwnd` | `8192` | پنجره دریافت |
| `transport.kcp.sndwnd` | `8192` | پنجره ارسال |
| `transport.kcp.smuxbuf` | `4MB` | بافر مالتی‌پلکس هر session |
| `transport.kcp.streambuf` | `256KB` | بافر هر stream |
| `transport.kcp.max_sessions` | `2048` | حداکثر session (سرور) |
| `transport.kcp.max_streams_total` | `65536` | حداکثر کل stream‌ها (سرور) |
| `transport.kcp.max_streams_per_session` | `4096` | حداکثر stream هر session |

### Guard (محافظ پکت)

| پارامتر | پیش‌فرض | توضیح |
|---------|---------|-------|
| `transport.kcp.guard` | `true` | فعال/غیرفعال |
| `transport.kcp.guard_magic` | `"PQT1"` | مجیک 4 بایتی (باید یکسان) |
| `transport.kcp.guard_window` | `30` | پنجره چرخش cookie (ثانیه) |
| `transport.kcp.guard_skew` | `1` | تعداد پنجره‌های قبلی مجاز |

### فایل‌های مهم روی سرور

| فایل | چیه |
|------|-----|
| `/etc/paqet/config.yaml` | کانفیگ اصلی |
| `/usr/local/bin/paqet` | باینری |
| `/usr/local/bin/paqet-ui` | اسکریپت منوی مدیریت |
| `/etc/systemd/system/paqet.service` | سرویس systemd |
| `/etc/sysctl.d/99-paqet.conf` | تنظیمات کرنل |

### دستورات CLI

```bash
sudo paqet run -c /etc/paqet/config.yaml   # اجرای مستقیم
sudo paqet version                          # نمایش ورژن
sudo paqet secret                           # تولید کلید جدید
sudo paqet ping -c /etc/paqet/config.yaml   # تست اتصال
sudo paqet status                           # وضعیت (اگه debug فعاله)
sudo paqet dump -p 9999                     # ضبط پکت‌ها (مثل tcpdump)
```

### دستورات سرویس

```bash
sudo systemctl status paqet     # وضعیت
sudo systemctl restart paqet    # ریستارت
sudo systemctl stop paqet       # توقف
sudo journalctl -u paqet -f     # لاگ زنده
```

---

## حذف

```bash
# حذف با حفظ باینری
sudo paqet-ui  # → گزینه 7

# حذف کامل (همه چیز)
sudo paqet-ui  # → گزینه 13

# یا با دستور:
curl -fsSL https://raw.githubusercontent.com/changecoin938/Tunnel/main/install.sh | sudo bash -s -- purge
```

---

## لایسنس

MIT License - فایل [LICENSE](LICENSE) رو ببینید.
