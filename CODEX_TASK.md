# PostgreSQL -> Typesense Sync POC

Bu proje, PostgreSQL'deki `products` tablosundan Typesense `products` koleksiyonuna veri senkronizasyonunu iki basit yontemle gosterir:

- Polling sync: Go worker `updated_at` alanina gore degisen product kayitlarini okur.
- Outbox sync: PostgreSQL trigger'i `search_outbox` tablosuna event yazar, Go worker eventleri isler.

POC bilincli olarak sade tutulmustur. Kafka, Debezium, CDC, generic repository veya production-heavy abstraction yoktur.

Beklenen temel akis:

```text
docker compose up -d
go run ./cmd/polling-sync
go run ./cmd/outbox-sync
```

Detayli calistirma ve test komutlari icin `README.md` dosyasina bak.
