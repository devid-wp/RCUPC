# RCUPC — Remote Control PC

Лёгкий remote-desktop для управления Windows PC с Linux-ноутбука по локальной сети.
Написан на Go. Основной приоритет — **LAN и минимальная задержка**, а не Internet/cloud.

```
Windows PC  ──── LAN ────▶  Linux laptop
 (Host)                      (Client)
```

## Статус

**Phase 1 реализован:** Host и Client соединяются по TCP и выполняют handshake
(`MIRROR/0.1 HELLO` → `MIRROR/0.1 OK`); соединение остаётся открытым. Только стандартная
библиотека, без зависимостей.

Следующая по плану — Phase 2, application protocol. См. [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Phase 1 — быстрый старт

### Windows (Host)

```powershell
go build -o rcupc-host.exe ./cmd/rcupc-host
.\rcupc-host.exe            # слушает :9000
.\rcupc-host.exe -addr :9000
```

Узнать локальный IP:

```powershell
ipconfig                  # смотреть "IPv4-адрес" активного адаптера, напр. 192.168.1.42
Get-NetIPAddress -AddressFamily IPv4   # альтернатива
```

### Linux (Client)

```bash
go build -o rcupc-client ./cmd/rcupc-client
./rcupc-client <WINDOWS_IP>:9000
```

Прервать — `Ctrl+C`; сессия закрывается корректно.

### Кросс-сборка host под Windows с Linux

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/rcupc-host.exe ./cmd/rcupc-host
```

### Пример успешного соединения

Host (Windows):

```
time=... level=INFO msg=listening addr=[::]:9000
time=... level=INFO msg="client connected" remote=192.168.1.77:51422
time=... level=INFO msg="handshake complete" remote=192.168.1.77:51422 protocol=MIRROR/0.1
time=... level=INFO msg="session closed" remote=192.168.1.77:51422
```

Client (Linux):

```
$ ./rcupc-client 192.168.1.42:9000
time=... level=INFO msg=connecting addr=192.168.1.42:9000
time=... level=INFO msg="connection established" addr=192.168.1.42:9000
^C
time=... level=INFO msg="connection closed" addr=192.168.1.42:9000
```

Неверный handshake — host сообщает причину и закрывает соединение:

```
$ printf 'WRONG/1.0 HELLO\n' | nc 192.168.1.42 9000
MIRROR/0.1 ERROR protocol: unsupported version "WRONG/1.0" (want "MIRROR/0.1")
```

## Цели

- Real-time просмотр рабочего стола Windows.
- Минимальная задержка видео.
- Инъекция mouse и keyboard событий.
- В перспективе — audio.
- Управление, близкое к «сижу за этим ПК».
- Понятная модульная архитектура, которую можно писать и понимать постепенно.

## Не-цели (сейчас)

- Internet / relay / cloud / STUN / TURN.
- Web-приложение.
- Electron.
- Сторонний сервер.
- Монолит.
- Полная security-система на ранних этапах.

## Карта документации

| Документ | Содержание |
|---|---|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Компоненты, схема, структура директорий, интерфейсы, потоки данных, сравнение transport и capture, video/input pipeline, риски |
| [`docs/PROTOCOL.md`](docs/PROTOCOL.md) | Собственный application protocol RCUPC: сообщения, framing, handshake, reconnect, heartbeat |
| [`docs/SECURITY.md`](docs/SECURITY.md) | Модель угроз, pairing, authentication, encryption, authorization |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | Phase 0 … Phase 12: цель, компоненты, результат, чему учит |

## Рекомендуемое чтение

1. `ARCHITECTURE.md` — понять границы компонентов и почему они такие.
2. `PROTOCOL.md` — понять, что именно летит по сети.
3. `SECURITY.md` — понять, какие «крючки» оставить уже сейчас.
4. `ROADMAP.md` — понять, в каком порядке всё это писать.

## Быстрые ключевые решения (обоснования внутри документов)

- **Capture:** GDI как временный старт → DXGI Desktop Duplication как основной путь.
- **Encode:** MJPEG для первого кадра → H.264 (hardware) для realtime.
- **Transport:** TCP для control/input → отдельный UDP data plane для видео → опционально QUIC.
- **Input host:** `SendInput` (user32), без kernel-драйверов.
- **Render Linux:** OpenGL/GLFW или Ebiten на старте, за интерфейсом `Renderer`.
- **Язык:** Go; платформенные куски — за build tags; узкие cgo/C++ shim'ы там, где иначе нельзя.
