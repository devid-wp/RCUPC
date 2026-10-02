# RCUPC — Roadmap

Последовательность разработки от пустого репозитория до полноценного LAN remote-desktop.
Каждая фаза даёт **работающий результат** и знания, на которые опирается следующая.

Формат каждой фазы: **Цель → Компоненты → Готово когда → Знания**.

---

## Phase 0 — Repository / project structure

**Цель:** каркас проекта, инструменты, пустые пакеты и интерфейсы.

**Компоненты:**
- `go.mod`, структура `cmd/ pkg/ host/ client/ docs/`.
- Пустые пакеты и интерфейсы (`Capturer`, `Encoder`, `Transport`, `Decoder`, `Renderer`,
  `Backend`, `Capturer` клиента).
- `pkg/config`, `pkg/logging`, `pkg/rerrors`.
- Makefile/скрипты сборки host (Windows) и client (Linux); `.gitignore`.
- CI-заготовки: vet, build под две ОС.

**Готово когда:** `go build ./...` проходит на обеих ОС; `rcupc-host --help` и
`rcupc-client --help` печатают заглушки.

**Знания:** организация Go-проекта, build tags, кросс-сборка, интерфейсы как границы,
`log/slog`.

---

## Phase 1 — LAN connection + handshake

**Цель:** host и client находят друг друга по LAN и устанавливают соединение.

**Компоненты:**
- `pkg/transport` (TCP) + `pkg/protocol` (минимальный HELLO/HELLO_ACK, framing).
- `host/server` (listener, accept), `host/session` (каркас).
- `client/session` (connect), `client/app` (CLI-заглушка).
- version negotiation и базовые capabilities.

**Готово когда:** client подключается к host по IP:port, обменивается HELLO и печатает
согласованную версию; некорректная версия даёт внятную ошибку.

**Знания:** TCP в Go, goroutines, `context`, framing по длине, handshake, версионирование,
чтение/запись сокетов, обработка ошибок.

---

## Phase 2 — Application protocol

**Цель:** полноценный минимальный протокол поверх соединения.

**Компоненты:**
- Все CONTROL-сообщения, CAPS, SESSION_INIT/ACCEPT, PING/PONG, ERROR, BYE.
- Сериализация/десериализация, TLV/флаги, коды ошибок.
- Heartbeat и RTT-оценка.
- Юнит-тесты на round-trip и версионирование.

**Готово когда:** сессии проходят полный handshake → session init → ping-сессия → BYE;
есть тесты; протокол описан и совпадает с `PROTOCOL.md`.

**Знания:** бинарные форматы, `encoding/binary`, TLV, write/read deadlines, тестирование
протоколов, fuzz-мышление.

---

## Phase 3 — Desktop capture (Windows)

**Цель:** получить живой кадр рабочего стола и сохранить его.

**Компоненты:**
- `host/capture` + первая реализация **GDI BitBlt** (CPU-буфер).
- Конвертация формата в пригодный для сохранения (например, PNG).
- CLI-режим host: «снять кадр и записать в файл».
- Метрики capture time.

**Готово когда:** на Windows создаётся корректный скриншот рабочего стола одного монитора.

**Знания:** Win32 API, `x/sys/windows`, собственно дескрипторы/BitBlt, форматы пикселей,
stride, DPI.

---

## Phase 4 — First frame transmission

**Цель:** передать один кадр host → client и показать его.

**Компоненты:**
- Простейший «VIDEO» кадр (raw/сжатый PNG) по TCP.
- `client/decode` (заглушка: распаковка), `client/render` (окно + вывод кадра).
- Mux в `host/session`, demux в `client/session`.
- Первый вариант `Renderer` (Ebiten или OpenGL/GLFW).

**Готово когда:** на Linux открывается окно и в нём виден снимок экрана Windows.

**Знания:** передача бинарных данных, управление размером сообщения, GUI/окно в Go,
отрисовка изображения, связь network ↔ UI (main thread).

---

## Phase 5 — Video codec

**Цель:** поток кадров вместо одного снимка; первый кодек.

**Компоненты:**
- `host/encode` + **MJPEG** (или raw по TCP как промежуточный шаг).
- `client/decode` + MJPEG.
- Latest-frame slot, drop-политика, простые метрики fps/latency.
- Возможно, снижение разрешения/quality.

**Готово когда:** видео рабочего стола обновляется 30+ fps с приемлемой задержкой по TCP.

**Знания:** кодирование изображений/видео, JPEG/H.264 основы, буферизация и backpressure,
drop vs queue, измерение latency.

---

## Phase 6 — Realtime streaming

**Цель:** уйти на hardware H.264 и UDP data plane, получить настоящий realtime.

**Компоненты:**
- `host/capture/dxgi` (Desktop Duplication, GPU-текстура).
- `host/encode/h264_mf` (Media Foundation hardware), при нужде NVENC.
- UDP data plane: фрагментация, frame_id, reassembly, latest-frame; опционально FEC/NACK.
- `client/decode` hardware/software H.264 (ffmpeg).
- `KEYFRAME_REQUEST` при потерях; keyframe/GOP-настройки.

**Готово когда:** H.264 1080p 60 fps по UDP с низкой и стабильной задержкой; падение Wi-Fi
восстанавливается keyframe'ом.

**Знания:** DXGI/D3D11, hardware encoding, H.264 NAL/bitstream, UDP, MTU, FEC/NACK,
jitter buffer, zero-copy GPU-пути.

---

## Phase 7 — Mouse

**Цель:** управлять курсором Windows с Linux.

**Компоненты:**
- `client/input` (X11/XInput2 захват, absolute-режим).
- INPUT-сообщения: move/button/wheel.
- `host/input` + `SendInput` (absolute), отдельная приоритетная goroutine.
- Локальная отрисовка курсора на клиенте.

**Готово когда:** движение и клики мыши на клиенте двигают Windows-курсор и нажимают кнопки.

**Знания:** X11/XInput2, coordinate spaces, DPI, `SendInput`, `MOUSEEVENTF_ABSOLUTE`,
приоритизация ввода над видео.

---

## Phase 8 — Keyboard

**Goal:** ввод с клавиатуры.

**Компоненты:**
- INPUT_KEY (scancode/vkey/modifiers), INPUT_TEXT (unicode), autorepeat.
- Захват клавиатуры на клиенте (grab, release-all).
- Host: `SendInput` с scancode, корректные modifiers.
- Горячая клавиша выхода/разрыва.

**Готово когда:** можно печатать в Windows-приложениях (латиница и кириллица), работают
Ctrl/Alt/Shift/Win, нет «залипаний» при разрыве.

**Знания:** keycodes/scancodes, раскладки, modifier state, keyboard grab, безопасный release.

---

## Phase 9 — Latency optimization

**Goal:** уменьшить и стабилизировать задержку.

**Компоненты:**
- Метрики по этапам и графики распределения (p50/p95/p99).
- Zero-copy GPU-путь, пулы буферов, отказ от лишних копий/аллокаций.
- Настройка encoder под low-latency (CBR, 0 B-frames, lookahead=0, VBV).
- Render без лишних буферов/vsync-нюансов.
- Возможно, пересмотр GOP/intra-refresh, fragment size.
- Батчинг input, приоритеты.

**Готово когда:** задержка снижена и стабильна; известен вклад каждого этапа; нет GC-дрожания.

**Знания:** профайлинг Go (`pprof`), latency budget, zero-copy, управление памятью, tuning
видео-encoder'ов, сетевая телеметрия.

---

## Phase 10 — Audio (future)

**Цель:** передавать звук Windows → Linux.

**Компоненты:**
- `host/audio` (WASAPI loopback) + Opus encode.
- AUDIO-сообщения, отдельный канал/jitter buffer.
- `client` Opus decode + воспроизведение (PulseAudio/PipeWire/ALSA).
- Синхронизация/расхождение аудио и видео (простейшая).

**Готово когда:** звук системы Windows воспроизводится на Linux с приемлемой задержкой.

**Знания:** WASAPI, Opus, audio pipeline, буферизация, A/V sync, Linux audio APIs.

---

## Phase 11 — Pairing / security

**Цель:** защита от посторонних и шифрование.

**Компоненты:**
- Pairing (код → ключи), хранение ключей (DPAPI / keyring).
- Mutual TLS для TCP, TLS в QUIC или Noise/DTLS для UDP.
- AUTH-сообщения, replay-защита.
- Permissions (view/control), consent UI, allow-list, idle lock.
- Индикатор активного управления и аварийный разрыв.

**Готово когда:** без pairing подключение невозможно; трафик зашифрован; ввод требует
разрешения; есть понятные отказ/согласие.

**Знания:** TLS 1.3 mutual auth, certificate pinning, PAKE, защита от replay,
хранение секретов, secure-by-default UX.

---

## Phase 12 — GUI / polish

**Цель:** сделать инструмент удобным.

**Компоненты:**
- Полноценный desktop GUI клиента (окно, настройки, список хостов, статус, статистика).
- Fullscreen, масштабирование, несколько мониторов, горячие клавиши.
- Reconnect, автоподбор параметров качества в зависимости от сети.
- Упаковка/установка (host installer на Windows, пакет/бинарь на Linux).
- Документация пользователя.

**Готово когда:** продукт можно запустить, подключиться, работать и настраивать без CLI.

**Знания:** desktop GUI в Go, состояние приложения, UX удалённого управления, дистрибуция,
логирование/диагностика у пользователя.

---

## Сводка зависимостей между фазами

```
P0 → P1 → P2 → P3 → P4 → P5 → P6 → P7 → P8 → P9 → P10 → P11 → P12
                   └──── GUI/render тянется с P4
                              └──── security-интерфейсы заложены с P1, реализация P11
                                        └──── audio независим после P6 (использует тот же mux)
```

Каждая фаза должна оставлять проект **собираемым и запускаемым**. Не начинать следующую,
пока текущий результат не проверен руками.
