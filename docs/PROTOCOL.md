# RCUPC — Application Protocol

Минимальный собственный бинарный протокол RCUPC. Документ описывает **дизайн**, не реализацию.
Реализация появится на Phase 2.

---

## 1. Принципы

1. **Бинарный, little-endian.** Фиксированные заголовки, без JSON в data plane. JSON допустим
   максимум во время отладки handshake и затем убирается.
2. **Два канала.** Control (надёжный, упорядоченный) и Data/Video (неупорядоченный, «лучше
   потерять, чем задержать»). Протокол не навязывает конкретный транспорт, а требует от него
   лишь эти два свойства.
3. **Самодостаточный заголовок.** По первым байтам всегда понятно: версия, тип, длина, флаги.
4. **Расширяемость.** Флаги + TLV для будущих полей. Незнакомый флаг/поле игнорируется, а не
   ломает парсинг.
5. **Версионирование.** major/minor; несовпадение major — отказ, minor — совместимость.
6. **Крипто-агностичность.** Протокол работает поверх уже установленного защищённого канала;
   сам он не шифрует (это делает transport layer).

## 2. Слои

```
  Application messages   HELLO, INPUT_MOUSE, VIDEO_FRAME_HEADER, PING, …
          │
          ▼  (serialize + framing)
  Frames / packets       общий заголовок + payload
          │
          ▼
  Transport              TCP stream  |  UDP datagrams  |  QUIC streams+datagrams
```

`pkg/protocol` владеет только двумя верхними слоями. Транспорт ничего не знает о смысле.

---

## 3. Заголовки

### 3.1. Общий (control) заголовок

Поля (примерный layout, все little-endian):

| Поле | Размер | Назначение |
|---|---|---|
| `magic` | 2 B | `0x52 0x43` («RC»), быстрая валидация |
| `version_major` | 1 B | несовместимые изменения |
| `version_minor` | 1 B | обратно совместимые |
| `msg_type` | 1 B | тип сообщения (таблица ниже) |
| `flags` | 2 B | флаги (compressed, encrypted, …) |
| `stream_id` | 1 B | логический поток: 0=control, 1=video, 2=audio, 3=input |
| `header_len` | 1 B | для расширений/TLV |
| `seq` | 4 B | порядковый номер в потоке |
| `ack` | 4 B | подтверждённый seq (для своих надёжных слоёв) |
| `length` | 4 B | длина payload |
| `timestamp_us` | 8 B | монотонное время отправителя, микросекунды |
| `payload` | *length* | данные |
| `tlv_ext` | *header_len − базовый* | опциональные расширения |

Control-сообщения обычно маленькие; заголовок фиксированный + опциональный TLV.

### 3.2. Заголовок video-пакета (data plane)

Видео живёт в datagrams и не должно тащить лишнее. Отдельный компактный заголовок:

| Поле | Размер | Назначение |
|---|---|---|
| `magic` | 2 B | `0x52 0x56` («RV») |
| `version` | 1 B | |
| `flags` | 1 B | bit0 keyframe, bit1 last_fragment, bit2 has_cursor, bit3 audio… |
| `frame_id` | 4 B | monotonically increasing, идентифицирует кадр |
| `fragment_index` | 2 B | номер фрагмента |
| `fragment_count` | 2 B | всего фрагментов |
| `payload_len` | 4 B | |
| `capture_ts_us` | 8 B | время захвата (для latency) |
| `encode_ts_us` | 8 B | опционально (два последних поля могут быть в TLV) |
| `payload` | *payload_len* | кусок bitstream |

Почему два заголовка: у видео другая семантика (фрагменты одного кадра, keyframe) и другой
бюджет на overhead. Единый «толстый» заголовок на каждый из ~1200-байтных пакетов — лишние
проценты пропускной способности и latency.

### 3.3. MTU и фрагментация

- Целевой payload — около **1200 байт**, чтобы избежать IP-фрагментации (типичный MTU 1500).
- Кадр бьётся на `fragment_count` частей по `frame_id`.
- Получатель собирает по `frame_id`; если потерян любой фрагмент — кадр отбрасывается
  (для keyframe можно запросить повтор через NACK).
- Порядок фрагментов не гарантирован; сборка по `fragment_index`.
- «Latest frame wins»: если начал приходить кадр с большим `frame_id`, недособранный —
  выбрасывается.

---

## 4. Типы сообщений

### 4.1. CONTROL (stream 0)

| Тип | Направление | Назначение |
|---|---|---|
| `HELLO` | oba | запрос соединения, список версий, device id |
| `HELLO_ACK` | host | выбранная версия, session id, nonce |
| `AUTH_BEGIN` | client | начало аутентификации/pairing |
| `AUTH_CHALLENGE` | host | challenge |
| `AUTH_RESPONSE` | client | ответ |
| `AUTH_RESULT` | host | ok/fail + reason |
| `CAPS` | oba | возможности (bitmask/TLV) |
| `SESSION_INIT` | client | параметры сессии (codec, разрешение, режим ввода) |
| `SESSION_ACCEPT` | host | подтверждение + финальные параметры |
| `SESSION_REJECT` | host | отказ + причина |
| `CONFIG_UPDATE` | oba | смена параметров на ходу (bitrate, monitor) |
| `KEYFRAME_REQUEST` | client | «повтори keyframe» (следствие потерь) |
| `BYE` | oba | корректное завершение + причина |

### 4.2. VIDEO (stream 1)

| Тип | Назначение |
|---|---|
| `VIDEO_FRAME_HEADER` | (опционально, если кадр в надёжном канале) метаданные кадра + codec params |
| `VIDEO_FRAGMENT` | фрагмент кадра (в UDP-канале) |
| `VIDEO_KEYFRAME` | флаг keyframe (может быть в flags) |

В UDP-режиме используется заголовок из 3.2 без отдельного `VIDEO_FRAME_HEADER`.

### 4.3. INPUT (stream 3)

| Тип | Payload |
|---|---|
| `INPUT_MOUSE_MOVE_ABS` | x (u16 0..65535), y (u16) |
| `INPUT_MOUSE_MOVE_REL` | dx (i16), dy (i16) |
| `INPUT_MOUSE_BUTTON` | button (u8), pressed (u8) |
| `INPUT_MOUSE_WHEEL` | delta_v (i16), delta_h (i16) |
| `INPUT_KEY` | scancode (u16), vkey (u16), modifiers (u16), pressed (u8), autorepeat (u8) |
| `INPUT_TEXT` | UTF-8 строка (layout-независимый ввод) |
| `INPUT_RELEASE_ALL` | — (при потере фокуса/выходе из захвата) |

### 4.4. PING / PONG

| Тип | Назначение |
|---|---|
| `PING` | ping с `timestamp` |
| `PONG` | эхо `seq`/`timestamp` → RTT |

### 4.5. AUDIO (future, stream 2)

| Тип | Назначение |
|---|---|
| `AUDIO_FORMAT` | codec (Opus), sample rate, channels |
| `AUDIO_FRAME` | сжатый аудио-пакет + seq + ts |

Аудио по требованиям к latency ближе к видео (datagrams, свой jitter buffer), но допускает
небольшой буфер.

### 4.6. ERROR

| Поле | Назначение |
|---|---|
| `code` | типизированный код (`rerrors`) |
| `severity` | warning/fatal |
| `message` | человекочитаемое пояснение (UTF-8, отладка) |
| `related_seq` | к какому сообщению относится |

---

## 5. Handshake и version negotiation

### 5.1. Последовательность

```
client                                   host
  │  TCP connect (или QUIC handshake)      │
  │ ────────── HELLO ────────────────────▶ │
  │            (versions[], device_id)     │
  │ ◀───────── HELLO_ACK ───────────────── │
  │            (version, session_id, nonce)│
  │ ────────── AUTH_BEGIN ───────────────▶ │   (Phase 11; до этого пропускается)
  │ ◀───────── AUTH_CHALLENGE ──────────── │
  │ ────────── AUTH_RESPONSE ────────────▶ │
  │ ◀───────── AUTH_RESULT ─────────────── │
  │ ────────── CAPS ─────────────────────▶ │
  │ ◀───────── CAPS ────────────────────── │
  │ ────────── SESSION_INIT ─────────────▶ │
  │ ◀───────── SESSION_ACCEPT ──────────── │
  │ ══════════ VIDEO (UDP) ═══════════════▶ │
  │ ────────── INPUT (TCP) ──────────────▶ │
  │ ────────── PING/PONG ────────────────▶ │
```

### 5.2. Version negotiation

- Каждая сторона объявляет диапазон `(min_major, max_major, minor)`.
- Выбирается максимальный общий major; при равенстве — максимальный общий minor.
- Нет общего major → `BYE` с `UNSUPPORTED_VERSION`.
- Minor несовместимости не создаёт: незнакомые поля/флаги игнорируются.

### 5.3. Capabilities

Набор TLV/bitmask, минимум:

- codecs: MJPEG, H.264 (и профили/уровни), позже H.265;
- capture backend: DXGI, WGC, GDI (для телеметрии/деградации);
- max resolution / max fps;
- monitors (число/список);
- input modes: absolute, relative, text;
- audio: none/Opus;
- features: clipboard (future), file transfer (future, не в текущих целях);
- security: TLS, Noise, none (для совместимости фаз).

Обе стороны пересекают множества; `SESSION_INIT` выбирает из пересечения.

### 5.4. Session initialization

`SESSION_INIT` (client → host):
- выбранный codec + параметры (bitrate, fps, GOP, intra-refresh);
- разрешение/монитор;
- режим ввода;
- желаемый канал (video: TCP/UDP/QUIC);
- предпочтения audio.

`SESSION_ACCEPT` (host → client): финальные параметры, фактические размеры, encoder id,
начальный keyframe будет отправлен первым.

---

## 6. Framing

- **Over TCP (control/input):** каждое сообщение = заголовок (с `length`) + payload. Читатель
  сначала читает фиксированный заголовок, затем ровно `length` байт. Никаких разделителей.
- **Over UDP (video):** каждый datagram = один video-пакет из 3.2. Никаких «частей сообщения
  из нескольких datagram» кроме явной фрагментации кадра.
- **Over QUIC:** control/input — отдельный stream (framing как TCP); video — datagrams (как UDP).
- **Buffers:** для парсинга используются пулы; payload может быть слайсом в приёмном буфере,
  чтобы не копировать (но тогда важно возвращать буфер в пул после обработки).

### 6.1. Timestamp и latency

`timestamp_us` — монотонное время отправителя. Для end-to-end в video-заголовке есть
`capture_ts_us`; клиент может вычислить `now − capture` с поправкой на оценку смещения часов
(полученную из PING/PONG). Это даёт практическую оценку end-to-end latency.

---

## 7. Disconnect и reconnect

### 7.1. Disconnect

- Нормальное: `BYE{reason}` → закрыть транспорт.
- Аварийное: heartbeat timeout / TCP reset / UDP молчание → session teardown.
- Host всегда отпускает все зажатые клавиши/кнопки (`ReleaseAll`) при разрыве, чтобы не
  «залипал» ввод.

### 7.2. Reconnect

План (не обязателен на ранних фазах):
- session token, выданный при `HELLO_ACK`, позволяет возобновить сессию без полного handshake;
- экспоненциальный backoff с jitter;
- окно «resume», после которого — полная переинициализация;
- после reconnect host обязан прислать keyframe; входные события не «догоняются» (они
  актуальны только в моменте).
- клиент переспрашивает caps, если host мог перезапуститься с другой версией/encoder'ом.

---

## 8. Heartbeat

- `PING` каждые N секунд (например, 1–2 с) по control-каналу.
- `PONG` возвращает тот же seq/ts → RTT и jitter.
- Если K подряд ping без pong (или RTT > порога) → считать сессию мёртвой → reconnect.
- Отдельный, более частый «video liveness» можно не делать: молчание видео-канала само по
  себе сигнал, но control-heartbeat обязателен, т.к. вход зависит от него.
- Heartbeat также двигает оценку смещения часов для latency.

---

## 9. Версионирование протокола

- `major` — ломающие изменения формата сообщений.
- `minor` — добавление новых типов/полей; старые игнорируют неизвестное.
- Новые message types резервируются в диапазонах (control/video/input/audio/error), чтобы
  не пересекаться.
- Все enum'ы имеют `UNKNOWN = 0` и обрабатываются безопасно.

---

## 10. Что не делаем сейчас

- Не реализуем протокол (Phase 2).
- Не проектируем file transfer, clipboard, multi-session, remote audio mixing — вне целей.
- Не вводим бинарную совместимость «навсегда»: достаточно аккуратного версионирования.
