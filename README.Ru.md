# dlink-snmp-exporter

[English](README.md) | **Русский**

Prometheus-экспортер для коммутаторов D-Link серий DES/DGS. Данные собираются по SNMP (v2c или v3).
Экспортер работает в multi-target режиме, как `snmp_exporter`: один процесс опрашивает любое число коммутаторов,
а Prometheus передаёт адрес коммутатора в `/probe?target=`.

Проверено на коммутаторах:

| Модель | Прошивка | Что доступно |
|---|---|---|
| **DGS-3120-24TC** (управляемый) | 3.12.R020 | всё: интерфейсы, EtherLike, RMON, CPU/память/температура/вентиляторы/БП, SFP DDM, MAC-таблица, LLDP |
| **DGS-1500-28** (smart) | 2.51.B022 | интерфейсы, EtherLike, MAC-таблица, LLDP, модель и прошивка. CPU, температуру и DDM по SNMP не отдаёт |

Другие управляемые DES/DGS-3xxx (DES-3200, DGS-3000, DGS-3420, DGS-3620 и т.д.) используют те же общие MIB D-Link
(`AGENT-GENERAL-MIB`, `EQUIPMENT-MIB`, DDM), поэтому, скорее всего, тоже работают, но не проверялись.
Экспортер определяет возможности сам: если коммутатор не реализует какую-то таблицу, соответствующий коллектор
не выдаёт метрик и считается успешным.

## Что собирается

| Коллектор    | Источник                                       | Что даёт |
|--------------|------------------------------------------------|----------|
| `system`     | SNMPv2-MIB, AGENT-GENERAL-MIB, EQUIPMENT-MIB    | модель, серийный номер, прошивка, железо, bootrom, MAC, uptime |
| `interfaces` | IF-MIB ifTable/ifXTable                         | статус, скорость, MTU, все счётчики трафика (64 бит): байты, unicast/multicast/broadcast, ошибки, дискарды, unknown protocols |
| `etherlike`  | EtherLike-MIB dot3StatsTable                    | duplex и ошибки Ethernet по типам: FCS, alignment, коллизии (в том числе late), symbol и др. |
| `rmon`       | RMON-MIB etherStatsTable                        | гистограмма размеров кадров, undersize/oversize/fragments/jabbers, CRC, drop events |
| `health`     | AGENT-GENERAL-MIB, EQUIPMENT-MIB                | CPU (5s/1m/5m), DRAM/flash, температура и пороги, вентиляторы, блоки питания |
| `ddm`        | DDM D-Link `171.12.72`                          | SFP: температура, напряжение, ток смещения, Tx/Rx (dBm), пороги alarm/warning |
| `lldp`       | LLDP-MIB lldpRemTable                           | соседи по портам |
| `fdb`        | Q-BRIDGE-MIB / BRIDGE-MIB                       | **таблица MAC-адресов**: MAC, VLAN, порт, статус, а также количество MAC на порту |

`fdb` не входит в модуль `default`. MAC-таблица опрашивается отдельным модулем `fdb` с другим интервалом,
потому что каждая запись становится отдельной серией.

## Быстрый старт

```bash
make build
./bin/dlink_exporter --config.file=config.example.yml
curl 'http://localhost:9841/probe?target=192.0.2.10'
curl 'http://localhost:9841/probe?target=192.0.2.10&module=fdb'
```

### Docker Compose: экспортер + Prometheus + Grafana

В `deploy/` лежит готовый стек: экспортер, Prometheus и Grafana с подключённым дашбордом
(он открывается как домашняя страница Grafana).

```bash
cd deploy
cp .env.example .env                                                  # по желанию
cp prometheus/targets/dlink.yml.example prometheus/targets/dlink.yml  # список коммутаторов
docker compose up -d
```

Адреса: Grafana http://localhost:3000, Prometheus http://localhost:9090, экспортер http://localhost:9841.
Изменения в `prometheus/targets/dlink.yml` Prometheus подхватывает сам. Конфиг экспортера лежит в
`deploy/dlink/config.yml`. У стека фиксированное имя проекта `dlink-snmp-exporter`, поэтому он не пересекается
с другими compose-стеками, запущенными из каталога `deploy`. Порты 9090 и 3000 при этом общие: если на хосте уже
работают другие Prometheus и Grafana, задайте другие значения `PROMETHEUS_PORT` и `GRAFANA_PORT`.

Все переменные необязательны, значения по умолчанию заданы в `docker-compose.yml`:

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `DLINK_EXPORTER_TAG` | `1.0.0` | тег образа `andrewpinkerton/dlink-snmp-exporter` |
| `EXPORTER_LOG_LEVEL` | `info` | уровень логов экспортера |
| `SNMP_COMMUNITY` | `public` | SNMPv2c community, подставляется в `dlink/config.yml` |
| `SNMP_AUTH_PASSWORD`, `SNMP_PRIV_PASSWORD` | пусто | пароли SNMPv3 (блок v3 в `dlink/config.yml` закомментирован) |
| `GRAFANA_ADMIN_USER`, `GRAFANA_ADMIN_PASSWORD` | `admin` / `admin` | администратор Grafana; смените пароль, прежде чем открывать Grafana наружу |
| `EXPORTER_PORT`, `PROMETHEUS_PORT`, `GRAFANA_PORT` | `9841`, `9090`, `3000` | публикуемые порты |
| `PROMETHEUS_RETENTION` | `30d` | сколько Prometheus хранит данные |
| `TZ` | `UTC` | часовой пояс Grafana |

Чтобы собрать экспортер из исходников, а не скачивать образ:
`docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build`.

## Конфигурация

```yaml
auths:
  public_v2:
    version: 2
    community: public
  secure_v3:
    version: 3
    username: monitor
    security_level: authPriv        # noAuthNoPriv | authNoPriv | authPriv
    auth_protocol: SHA              # MD5 | SHA | SHA224 | SHA256 | SHA384 | SHA512
    auth_password: ${SNMP_AUTH_PASSWORD}
    priv_protocol: AES              # DES | AES | AES192 | AES256 | AES192C | AES256C
    priv_password: ${SNMP_PRIV_PASSWORD}

modules:
  default:                          # system, interfaces, etherlike, rmon, health, ddm, lldp
    timeout: 5s                     # на один SNMP-запрос
    retries: 1
    max_repetitions: 25
    interface_types: [6, 117, 161]  # ifType: ethernetCsmacd, gigabitEthernet, ieee8023adLag; [] = все
  fdb:
    collectors: [fdb]
    fdb_max_entries: 20000          # больше записей: коллектор падает, MAC не выгружаются
```

- `auth` в запросе можно не указывать, если в конфиге один auth. `module` по умолчанию `default`.
- `interface_types` нужен, потому что D-Link перечисляет в ifTable VLAN-интерфейсы
  (`802.1Q Encapsulation Tag 0001`) и интерфейс `System`. Фильтр применяется к `interfaces`, `etherlike` и `rmon`.
- `fdb_max_entries` защищает Prometheus от MAC-флуда: при превышении лимита экспортируется только количество MAC
  на портах, а `dlink_collector_success{collector="fdb"}` становится 0.
- Секреты подставляются из окружения только в форме `${VAR}` (одиночный `$` в пароле не трогается).
  Если переменная не задана, конфиг не загрузится.
- Перечитать конфиг: `SIGHUP` или `POST /-/reload`. Проверить конфиг без запуска: `--config.check`.

### Флаги

| Флаг                          | По умолчанию     | Описание |
|-------------------------------|------------------|----------|
| `--config.file`               | `config.yml`     | файл конфигурации |
| `--web.listen-address`        | `:9841`          | адрес HTTP |
| `--web.config.file`           |                  | TLS/basic-auth ([exporter-toolkit](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md)) |
| `--scrape.timeout-offset`     | `500ms`          | вычитается из `X-Prometheus-Scrape-Timeout-Seconds` |
| `--log.level`, `--log.format` | `info`, `logfmt` | |

### Prometheus

```yaml
scrape_configs:
  - job_name: dlink
    scrape_interval: 30s
    scrape_timeout: 15s
    metrics_path: /probe
    params:
      module: [default]
    static_configs:
      - targets: [192.0.2.10, 192.0.2.11]
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: dlink-snmp-exporter:9841

  - job_name: dlink_fdb               # MAC-таблица
    scrape_interval: 1m
    scrape_timeout: 30s
    metrics_path: /probe
    params:
      module: [fdb]
    static_configs:
      - targets: [192.0.2.10, 192.0.2.11]
    relabel_configs: # те же, что выше
```

Полный пример с file_sd лежит в `deploy/prometheus/prometheus.yml`.

## Метрики

Все метрики порта несут метки `ifindex`, `ifname`, `description` (ifAlias) и `port`. Метка `port` записывается так,
как порт показывает CLI D-Link: `1:24` на DGS-3xxx (unit:port), `24` на DGS-1500 (`Slot0/24`), `po1` для LAG.

### Интерфейсы

| Метрика | Тип | Описание |
|---|---|---|
| `dlink_interface_oper_up`, `dlink_interface_admin_up` | gauge | состояние порта |
| `dlink_interface_speed_bytes` | gauge | скорость, байт/с |
| `dlink_interface_mtu_bytes` | gauge | MTU |
| `dlink_interface_last_change_timestamp_seconds` | gauge | время последней смены статуса |
| `dlink_interface_{receive,transmit}_bytes_total` | counter | байты (ifHC*, при их отсутствии 32-битные) |
| `dlink_interface_{receive,transmit}_{unicast,multicast,broadcast}_packets_total` | counter | пакеты по типам |
| `dlink_interface_{receive,transmit}_{errors,discards}_total` | counter | ошибки и дискарды |
| `dlink_interface_receive_unknown_protocol_packets_total` | counter | отброшенные пакеты с неизвестным протоколом |
| `dlink_interface_duplex{duplex}` | gauge | state set: unknown, half, full |
| `dlink_interface_ethernet_errors_total{type}` | counter | alignment, fcs, single_collision, multiple_collision, sqe_test, deferred, late_collision, excessive_collision, internal_mac_transmit, carrier_sense, frame_too_long, internal_mac_receive, symbol |
| `dlink_rmon_packets_total{size}` | counter | кадры по размеру: 64, 65_127, 128_255, 256_511, 512_1023, 1024_1518 |
| `dlink_rmon_{drop_events,crc_align_errors,undersize_packets,oversize_packets,fragments,jabbers,collisions}_total` | counter | RMON-счётчики ошибок |

Октеты RMON не выгружаются: это 32-битный счётчик, который на гигабите переполняется примерно за 34 с.
Для байтов используйте `dlink_interface_*_bytes_total`.

### MAC-таблица (модуль `fdb`)

| Метрика | Описание |
|---|---|
| `dlink_fdb_entry{<порт>,vlan,mac,status}` | одна запись MAC-таблицы, значение всегда 1. `status`: learned, mgmt, self, other. MAC самого коммутатора привязан к `port="cpu"` |
| `dlink_fdb_entries{<порт>}` | количество MAC на порту |

Примеры запросов: найти порт по MAC — `dlink_fdb_entry{mac=~"00:24:97.*"}`; найти порты, где больше одного MAC
(неуправляемый свитч или петля), — `dlink_fdb_entries{port!="cpu"} > 1`.

### Система и здоровье

| Метрика | Описание |
|---|---|
| `dlink_device_info{model,serial,hardware,firmware,bootrom,firmware_type,mac,sysname,sysobjectid}` | паспорт устройства |
| `dlink_uptime_seconds` | время работы агента |
| `dlink_cpu_utilization_ratio{period="5s\|1m\|5m"}` | загрузка CPU |
| `dlink_memory_{total,used}_bytes{unit,type="dram\|flash"}` | память |
| `dlink_temperature_celsius{unit}`, `dlink_temperature_threshold_celsius{unit,type="high\|low"}` | температура и пороги |
| `dlink_fan_status{unit,fan,position,status}` | state set: other, working, fail, speed_0, speed_low, speed_middle, speed_high (`speed_0` означает, что вентилятор стоит, потому что прохладно) |
| `dlink_fan_failed{unit,fan,position}`, `dlink_fan_speed_rpm{...}` | отказ вентилятора и обороты |
| `dlink_power_supply_status{unit,psu,status}` | state set: other, low_voltage, over_current, working, fail, connect, disconnect |

### SFP / DDM

| Метрика | Описание |
|---|---|
| `dlink_sfp_temperature_celsius`, `dlink_sfp_voltage_volts`, `dlink_sfp_bias_current_amperes`, `dlink_sfp_tx_power_dbm`, `dlink_sfp_rx_power_dbm` | показания DDM (только порты с модулем, поддерживающим DDM) |
| `dlink_sfp_threshold{param,level}` | пороги модуля: `level` = high_alarm, high_warning, low_warning, low_alarm; `param` — имя метрики выше |

Нулевая оптическая мощность (нет света) выгружается как −40 dBm.

### LLDP

`dlink_lldp_neighbor_info{<порт>,chassis_id,port_id,port_description,system_name,system_description}` = 1.

### Служебные

`dlink_probe_success`, `dlink_probe_duration_seconds`, `dlink_collector_success{collector}`,
`dlink_collector_duration_seconds{collector}`. На `/metrics` экспортера:
`dlink_exporter_probes_total{module,result}`, `dlink_exporter_config_last_reload_successful`, `dlink_exporter_build_info`.

## Карта OID и особенности прошивок

| Данные | OID | Примечание |
|---|---|---|
| CPU | `1.3.6.1.4.1.171.12.1.1.6.{1,2,3}.0` | AGENT-GENERAL-MIB, % за 5s/1m/5m |
| Память | `1.3.6.1.4.1.171.12.1.1.{9,10}.1.{2,3}.<unit>` | DRAM/flash, всего/занято в КБ |
| Серийный номер, тип прошивки | `1.3.6.1.4.1.171.12.1.1.{12,13}.0` | |
| Блоки питания | `1.3.6.1.4.1.171.12.11.1.6.1.3.<unit>.<psu>` | EQUIPMENT-MIB swPowerStatus |
| Вентиляторы | `1.3.6.1.4.1.171.12.11.1.7.1.{3,4,5,6}.<unit>.<id>` | статус, положение, номер, об/мин |
| Температура | `1.3.6.1.4.1.171.12.11.1.8.1.{2,3,4}.<unit>` | текущая, верхний и нижний порог |
| Паспорт юнита | `1.3.6.1.4.1.171.12.11.1.9.4.1.{2,9,10,11,12,17}.<unit>` | MAC, модель, boot, fw, hw, serial |
| DDM показания | `1.3.6.1.4.1.171.12.72.2.1.1.1.{2..6}.<port>` | строки: °C, В, мА, мВт, мВт |
| DDM пороги | `1.3.6.1.4.1.171.12.72.3.1.1.1.{2..5}.<port>.<param>` | high alarm, low alarm, high warning, low warning |
| DGS-1500: железо, прошивка | `<sysObjectID>.1.2.0`, `<sysObjectID>.1.3.0` | приватная ветка smart-серии |
| MAC-таблица | `1.3.6.1.2.1.17.7.1.2.2.1.{2,3}.<vlan>.<mac>` | Q-BRIDGE; если она пуста, используется `17.4.3.1` |

Особенности, учтённые в коде:

- **DDM отдаётся строками** (`"0.1615"` мВт). Если в порту нет модуля с DDM, агент возвращает `"  -"`.
  Публичного MIB D-Link для этой ветки нет: смысл колонок выведен из данных и сверен с порогами модуля.
- **VLAN-интерфейсы и `System` в ifTable** у DGS-3120 отфильтровываются по `interface_types`.
- **DGS-1500 не отдаёт CPU, память, температуру, вентиляторы, БП и DDM.** Таких объектов нет ни в официальном MIB
  D-Link для DGS-1500-28 (2.50.002), ни в ответах агента. DGS-1500-28 к тому же безвентиляторный. На дашборде такие
  панели помечены как «not reported by this model».
- **DGS-1500: `dot3StatsSQETestErrors`** на всех портах содержит одно и то же бессмысленное число (~1.1e9),
  поэтому для серии DGS-1500 этот счётчик не выгружается.
- **DGS-1500: LAG (`po1…po14`)** есть только в ifXTable, без ifType и с нулевыми счётчиками. Не выгружаются.
- **DGS-3120: DRAM всегда занята на ~99 %.** Это особенность прошивки, а не утечка памяти. Смотрите на тренд.
- **DGS-3120: второй блок питания в статусе `fail`** означает, что резервный блок (RPS) не подключён.
- **DGS-3120 держит в MAC-таблице записи из VLAN, которые на нём не настроены** (например 19, 60, 2573).
  Они выгружаются как есть, с этим VLAN ID.
- **Скорость опроса:** DGS-3120 около 1.5 с на полный probe, DGS-1500 около 2 с (каждый запрос к агенту DGS-1500
  стоит около 0.05–0.1 с). Модуль `fdb` занимает 0.2–0.5 с. Таблицы, которых у коммутатора нет, проверяются
  одним запросом.
- **Недоступный коммутатор:** после первого тайм-аута остальные коллекторы пропускаются, и probe укладывается
  в тайм-аут скрейпа.

## Безопасность

- Используйте отдельный read-only community с ACL на адрес экспортера, а лучше SNMPv3 `authPriv`.
- Проверьте community с правом записи и удалите ненужные: экспортеру нужно только чтение. На проверенном DGS-1500
  кроме `public` настроен community `private` с view `ReadWrite`.
- На DGS-1500 через read-only community по SNMP видна таблица community и пользователей. Это ещё одна причина
  ограничить доступ к SNMP списком адресов.
- Веб-интерфейс экспортера можно закрыть TLS и basic-auth через `--web.config.file`.

## Деплой

- **Docker:** образ `andrewpinkerton/dlink-snmp-exporter` (distroless, non-root, linux/amd64 и linux/arm64).
  Конфиг монтируется в `/etc/dlink_exporter/config.yml`:

  ```bash
  docker run -d -p 9841:9841 -v $PWD/config.yml:/etc/dlink_exporter/config.yml:ro \
    andrewpinkerton/dlink-snmp-exporter:1.0.0
  ```

  Версии по SemVer (номер в файле `VERSION`). Каждый релиз публикуется одним тегом с точной версией.
  `make docker` собирает локальный образ, `make docker-push` пушит его, `make docker-release` собирает
  amd64+arm64 и сразу пушит. Для пуша нужен `docker login`.
- **systemd:** `deploy/systemd/dlink-snmp-exporter.service` (DynamicUser, hardening). Инструкция по установке
  в комментарии внутри файла. Секреты кладите в `/etc/default/dlink_exporter` с правами 0600.
- **Grafana:** дашборд `grafana/dlink.json`. В нём обзор устройства, таблица портов (статус, скорость, duplex,
  трафик, ошибки, число MAC), графики трафика и ошибок, RMON, SFP, **поиск по MAC-таблице** (переменные Port,
  VLAN, MAC), LLDP и состояние системы. Дашборд генерируется командой `make dashboard`.

## Разработка

```bash
make test       # unit + golden-тесты на реальных дампах testdata/*.walk
make lint       # gofmt + go vet
make golden     # перегенерировать internal/collector/testdata/<модель>/*.prom после осознанного изменения
make mibcheck   # MIB загружаются net-snmp, все приватные OID из дампов резолвятся
```

Фикстуры снимаются полным walk'ом, а затем обрезаются до нужных веток и анонимизируются (MAC, серийные номера,
sysName):

```bash
snmpbulkwalk -v2c -c public -On -Oe -Ot -Cc <switch> .1 > full.walk
python3 testdata/mkfixture.py full.walk > testdata/<model>.walk
```

## MIB

В `mibs/` лежат MIB для приватных объектов, которые читает экспортер:
- `AGENT-GENERAL-MIB`, `EQUIPMENT-MIB`, `DLINK-ID-REC-MIB`, `TIMERANGE-MIB` — MIB D-Link (из LibreNMS);
- `DLINK-DDM-RECONSTRUCTED-MIB` — наша реконструкция ветки DDM `171.12.72`, потому что MIB D-Link для неё
  не опубликован. Имена объектов придуманы нами;
- `deps/` — стандартные зависимости (Q-BRIDGE-MIB и др.), которых нет в поставке net-snmp.

Сам экспортер MIB не загружает, OID зашиты в код.

## Лицензия

[MIT](LICENSE)
