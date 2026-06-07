# PostgreSQL -> Typesense Sync POC

Bu proje, PostgreSQL'den Typesense'e veri senkronizasyonunun Go ile nasil calistigini gosteren sade bir POC'tur.

Amac production-grade karmasik bir sistem kurmak degil; polling ve outbox pattern yaklasimlarini okunabilir, debug edilebilir ve localde calisir sekilde gostermektir.

## 1. Proje Amaci

POC iki senkronizasyon yontemi icerir:

- **Polling Sync:** Go worker belirli araliklarla `products` tablosunu okur, `updated_at` alanina gore degisen kayitlari bulur, aktif urunleri Typesense'e upsert eder ve `is_deleted=true` olan urunleri Typesense'ten siler.
- **Outbox Pattern Sync:** PostgreSQL trigger'i product insert/update/delete islemlerinden sonra `search_outbox` tablosuna event yazar. Go worker eventleri okuyup Typesense'e upsert/delete gonderir.

PostgreSQL bu POC'ta primary database'tir. Typesense ise sadece search index olarak kullanilir.

## 2. Kullanilan Teknolojiler

- PostgreSQL 16
- Typesense 30.2
- Go
- `github.com/jackc/pgx/v5/pgxpool`
- Docker Compose
- Typesense HTTP API (`net/http`)

## 3. Mimari

```text
PostgreSQL products
        |
        | polling: updated_at taramasi
        v
Go polling worker -----> Typesense products collection

PostgreSQL products
        |
        | trigger
        v
search_outbox
        |
        | processed=false eventleri
        v
Go outbox worker -----> Typesense products collection
```

PostgreSQL product `id` degeri Typesense document `id` olarak kullanilir. Boylece ayni urun update edilirken `upsert`, silinirken `delete` dogrudan ayni document'a uygulanir.

## 4. Proje Yapisi

```text
typesense-poc/
├── README.md
├── CODEX_TASK.md
├── docker-compose.yml
├── go.mod
├── .env.example
├── .gitignore
├── sql/
│   ├── 001_init.sql
│   └── 002_outbox.sql
├── cmd/
│   ├── polling-sync/
│   │   └── main.go
│   └── outbox-sync/
│       └── main.go
└── internal/
    ├── config/
    ├── model/
    ├── postgres/
    ├── typesense/
    ├── polling/
    └── outbox/
```

## 5. Docker ile PostgreSQL + Typesense Baslatma

```powershell
docker compose up -d
```

Container kontrol:

```powershell
docker ps
```

Typesense health check:

```powershell
curl.exe http://localhost:8108/health
```

Beklenen cevap:

```json
{"ok":true}
```

PostgreSQL'e baglan:

```powershell
docker exec -it pg-typesense-poc psql -U postgres -d appdb
```

Product kontrol:

```sql
SELECT id, name, price, updated_at
FROM products;
```

Not: `sql/001_init.sql` ve `sql/002_outbox.sql` temiz PostgreSQL volume ile otomatik calisir. `002_outbox.sql` tekrar uygulanabilir sekilde yazilmistir.

## 6. Polling Sync Calistirma

```powershell
go run ./cmd/polling-sync
```

Worker baslayinca once seed urunleri Typesense'e gonderir. `lastSyncedAt` POC icin memory'de tutulur. Production'da bu deger DB'de kalici tutulmalidir.

## 7. Polling Testleri

Typesense'te seed data ara:

```powershell
curl.exe "http://localhost:8108/collections/products/documents/search?q=iphone&query_by=name,description" `
  -H "X-TYPESENSE-API-KEY: xyz"
```

Beklenen: `iPhone 15` gorunmeli.

Yeni product ekle:

```sql
INSERT INTO products(name, description, category, price, stock)
VALUES ('Dell XPS 13', 'Windows laptop', 'laptop', 62000, 8);
```

Typesense'te ara:

```powershell
curl.exe "http://localhost:8108/collections/products/documents/search?q=dell&query_by=name,description" `
  -H "X-TYPESENSE-API-KEY: xyz"
```

Beklenen: `Dell XPS 13` gorunmeli.

Update testi:

```sql
UPDATE products
SET price = 59000
WHERE name = 'Dell XPS 13';
```

Beklenen: Typesense sonucunda `price=59000` gorunmeli.

Soft delete testi:

```sql
UPDATE products
SET is_deleted = true
WHERE name = 'Dell XPS 13';
```

Beklenen: Typesense aramasinda `Dell XPS 13` gorunmemeli.

## 8. Outbox Pattern Calistirma

Polling worker'i durdur.

Outbox SQL'i manuel uygulamak istersen:

```powershell
docker exec -i pg-typesense-poc psql -U postgres -d appdb < .\sql\002_outbox.sql
```

Outbox worker'i calistir:

```powershell
go run ./cmd/outbox-sync
```

Worker `processed=false AND retry_count < 5` eventleri okur. Basarili eventlerde `processed=true` ve `processed_at=now()` yazar. Hatali eventlerde `retry_count` artar ve `error_message` guncellenir.

## 9. Outbox Testleri

Yeni product ekle:

```sql
INSERT INTO products(name, description, category, price, stock)
VALUES ('Logitech MX Master 3S', 'Wireless mouse', 'accessory', 4200, 20);
```

Outbox kontrol:

```sql
SELECT id, operation_type, processed, retry_count, error_message
FROM search_outbox
ORDER BY id DESC;
```

Beklenen: yeni event icin `processed=true`.

Typesense'te ara:

```powershell
curl.exe "http://localhost:8108/collections/products/documents/search?q=logitech&query_by=name,description" `
  -H "X-TYPESENSE-API-KEY: xyz"
```

Beklenen: `Logitech MX Master 3S` gorunmeli.

Update testi:

```sql
UPDATE products
SET price = 3900
WHERE name = 'Logitech MX Master 3S';
```

Beklenen: Typesense sonucunda `price=3900` gorunmeli.

Soft delete testi:

```sql
UPDATE products
SET is_deleted = true
WHERE name = 'Logitech MX Master 3S';
```

Beklenen: Typesense aramasinda kayit gorunmemeli.

Hard delete testi:

```sql
INSERT INTO products(name, description, category, price, stock)
VALUES ('Temporary Keyboard', 'Keyboard for delete test', 'accessory', 1000, 5);

DELETE FROM products
WHERE name = 'Temporary Keyboard';
```

Beklenen: `search_outbox` icinde delete event olusmali ve worker tarafindan islenmeli.

## 10. Polling vs Outbox Karsilastirmasi

| Konu | Polling | Outbox |
| --- | --- | --- |
| Basitlik | Cok basit | Bir tablo ve trigger gerekir |
| Degisiklik yakalama | `updated_at` ile tarar | Her degisiklik event olur |
| Delete destegi | Soft delete ile kolay | Soft delete ve hard delete destekler |
| POC debug | Kolay | Event tablosu sayesinde daha gorunur |
| Production ihtiyaci | Kalici cursor gerekir | Transaction, locking ve retry stratejisi gerekir |

## 11. Temiz Kurulum

Container ve volume'leri tamamen temizle:

```powershell
docker compose down -v
```

Tekrar baslat:

```powershell
docker compose up -d
```

`docker compose down -v` PostgreSQL ve Typesense datasini siler. Bastan temiz POC denemesi icin kullanilir.

## 12. Notlar ve Gelistirme Fikirleri

Bu POC'ta bilincli olarak sade bir tasarim kullanildi:

- Go worker'lar local `go run` ile calisir.
- Typesense icin resmi SDK yerine `net/http` kullanilir.
- Polling cursor memory'de tutulur.
- Outbox eventleri sade sekilde sirayla islenir.
- Transaction ve `FOR UPDATE SKIP LOCKED` zorunlu tutulmadi.

Production'a tasinacaksa eklenebilecekler:

- Go worker'i Dockerize etmek
- Config'i `.env` dosyasindan yuklemek
- Structured logging eklemek
- Outbox batch processing yapmak
- Transaction + `FOR UPDATE SKIP LOCKED` kullanmak
- Dead-letter event takibi yapmak
- Retry backoff eklemek
- Metrics/observability eklemek
- Integration test yazmak
- GitHub Actions eklemek
- Typesense schema migration yaklasimi eklemek

## Ek Test Komutlari

Go paketlerini derleme/test etme:

```powershell
go test ./...
```

Polling binary derleme kontrolu:

```powershell
go build ./cmd/polling-sync
```

Outbox binary derleme kontrolu:

```powershell
go build ./cmd/outbox-sync
```
