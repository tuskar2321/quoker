# deploy/pc: скрипты на ПК владельца

Эти скрипты работают только на ПК. Ничего отсюда не уходит на серверы без команды владельца.

| Файл | Что делает |
| --- | --- |
| `backup-pull.sh` | Забирает новые снимки БД ядра (`/opt/app/backups`) по SSH через rsync, хранит 30, `--verify` проверяет `PRAGMA integrity_check`. |
| `new-worker-cert.sh` | Выпускает клиентский сертификат воркера (`step`), печатает SHA-256 отпечаток и команды дальше. Ключ CA не покидает ПК. |
| `systemd/` | Таймер и сервис для Linux (при загрузке и раз в сутки). |
| `launchd/com.quoker.backup-pull.plist` | Агент launchd для macOS (при входе и раз в сутки). |

## Бэкапы: установка на macOS

1. Ключ только для копирования бэкапов. Создайте отдельный ключ и добавьте его публичную часть в `/home/deploy/.ssh/authorized_keys` на ядре:
   `ssh-keygen -t ed25519 -f ~/.ssh/quoker_backup_ed25519 -C quoker-backup`
2. Папка на зашифрованном диске, например `/Volumes/Encrypted/quoker-backups`.
3. Настройки в `~/.config/quoker/backup-pull.env` (вне репозитория):
   ```sh
   QUOKER_BACKUP_HOST=deploy@<ip-ядра>
   QUOKER_BACKUP_DEST=/Volumes/Encrypted/quoker-backups
   QUOKER_BACKUP_KEY=~/.ssh/quoker_backup_ed25519
   QUOKER_BACKUP_KEEP=30
   ```
4. Один раз примите ключ сервера: `ssh -i ~/.ssh/quoker_backup_ed25519 deploy@<ip-ядра> true`.
5. Установка агента:
   ```sh
   install -m 0755 deploy/pc/backup-pull.sh ~/.local/bin/quoker-backup-pull
   sed "s|__HOME__|$HOME|g" deploy/pc/launchd/com.quoker.backup-pull.plist > ~/Library/LaunchAgents/com.quoker.backup-pull.plist
   launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.quoker.backup-pull.plist
   ```
6. Ежемесячно: `~/.local/bin/quoker-backup-pull --verify` и восстановление снимка на тестовой копии.

Для Linux используйте `systemd/` (см. комментарии в unit-файлах).

## Сертификат воркера

```sh
deploy/pc/new-worker-cert.sh worker-2
```

Нужны `step` и CA в `~/quoker-ca` (`ca.crt`, `ca.key` с правами 600). Скрипт не подключается к серверам: он печатает команды, которые владелец выполняет сам.

## Важно

- Снимки содержат токен бота и cookies Instagram. Храните их только на зашифрованном диске.
- Ключ CA живёт только на ПК и в офлайн-копии.
