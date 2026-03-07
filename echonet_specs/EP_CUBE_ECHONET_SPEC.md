# EP Cube ECHONET Lite Specification Reference
**Device Model:** GZ-000900 (EP Cube)
**Manufacturer:** Sungrow (0x000131)
**Protocol:** ECHONET Lite Ver. 1.1

This document provides a clean mapping of the ECHONET Lite registers (EPCs) implemented by the EP Cube, consolidated from the official compliance documentation (PDF/PNG verification).

---

## 🏗️ 1. Node Profile Object (0x0EF001)
Basic device identification and node management.

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Notes / Values |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0x80` | 動作状態 | Operation status | 1B | Get | `0x30`=ON, `0x31`=OFF |
| `0x82` | 規格Version情報 | Version information | 4B | Get | `0x010E0100` (Ver 1.1) |
| `0x83` | 識別番号 | Identification number | 17B | Get | `0xFE` + `0x000131` + 13 bytes |
| `0x88` | 異常発生状態 | Fault status | 1B | Get | `0x41`=Fault, `0x42`=Normal |
| `0x8A` | メーカコード | Manufacturer code | 3B | Get | `0x000131` (Sungrow) |
| `0x9D` | 状変アナウンスプロパティマップ | Status change announcement property map | 3B | Get | `0x0280D5` |
| `0x9E` | Set プロパティマップ | Set property map | 1B | Get | `0x00` |
| `0x9F` | Get プロパティマップ | Get property map | 14B | Get | `0x0D808283888A9D9E9FD3D4D5D6D7` |
| `0xD3` | 自ノードインスタンス数 | Number of self-node instances | 3B | Get | `0x000002` |
| `0xD4` | 自ノードクラス数 | Number of self-node classes | 2B | Get | `0x0003` |
| `0xD5` | インスタンスリスト通知 | Instance list notification | 7B | Get | `0x02027D01027901` |
| `0xD6` | 自ノードインスタンスリストS | Self-node instance list S | 7B | Get | `0x02027D01027901` |
| `0xD7` | 自ノードクラスリストS | Self-node class list S | 5B | Get | `0x02027D0279` |

---

## 🔋 2. Storage Battery Object (0x027D01)
Primary interface for battery levels, power flow, and modes.

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Range / Values |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0x80` | 動作状態 | Operation status | 1B | Get | `0x30`=ON, `0x31`=OFF |
| `0x81` | 設置場所 | Installation location | 1B | Set/Get | Reference standard |
| `0x82` | 規格Version情報 | Standard version information | 4B | Get | `0x00005101` |
| `0x83` | 識別番号 | Identification number | 17B | Get | `0xFE` + `0x000131` + 13 bytes |
| `0x88` | 異常発生状態 | Fault status | 1B | Get | `0x41`=Fault, `0x42`=Normal |
| `0x8A` | メーカコード | Manufacturer code | 3B | Get | `0x000131` |
| `0x97` | 現在時刻設定 | Current time setting | 2B | Set/Get | `0x00`-`0x17`: `0x00`-`0x3B` |
| `0x98` | 現在年月日設定 | Current date setting | 4B | Set/Get | YYYY:MM:DD |
| `0x9D` | 状変アナウンスプロパティマップ | Status change announcement property map | 10B | Get | `0x09808188AAABC1C2CFDA` |
| `0x9E` | Set プロパティマップ | Set property map | 9B | Get | `0x08819798AAABC1C2DA` |
| `0x9F` | Get プロパティマップ | Get property map | 17B | Get | `0x2005155545440440021714252400020212` |
| `0xA0` | AC実効容量(充電) | AC effective capacity (charging) | 4B | Get | 0 - 999,999,999 Wh |
| `0xA1` | AC実効容量(放電) | AC effective capacity (discharging) | 4B | Get | 0 - 999,999,999 Wh |
| `0xA2` | AC充電可能容量 | AC chargeable capacity | 4B | Get | 0 - 999,999,999 Wh |
| `0xA3` | AC放電可能容量 | AC dischargeable capacity | 4B | Get | 0 - 999,999,999 Wh |
| `0xA4` | AC充電可能量 | AC chargeable amount | 4B | Get | 0 - 999,999,999 Wh |
| `0xA5` | AC放電可能量 | AC dischargeable amount | 4B | Get | 0 - 999,999,999 Wh |
| `0xA8` | AC積算充電電力量計測値 | Cumulative AC charge amount | 4B | Get | 0 - 999,999,999 Wh |
| `0xA9` | AC積算放電電力量計測値 | Cumulative AC discharge amount | 4B | Get | 0 - 999,999,999 Wh |
| `0xAA` | AC充電量設定値 | AC charge amount setting | 4B | Set/Get | 1 - 999,999,999 Wh, `0x00`=Not set |
| `0xAB` | AC放電量設定値 | AC discharge amount setting | 4B | Set/Get | 1 - 999,999,999 Wh, `0x00`=Not set |
| `0xC1` | 充電方式 | Charging method | 1B | Set/Get | `0x01`=Maximum charging electric power charging, `0x02`=Surplus electric power charging, `0x03`=Designated electric power charging, `0x04`=Designated electric current charging, `0x00`=Others |
| `0xC2` | 放電方式 | Discharging method | 1B | Set/Get | `0x01`=Maximum discharging electric power discharging, `0x02`=Load following discharging, `0x03`=Designated electric power discharging, `0x04`=Designated electric current discharging, `0x00`=Others |
| `0xC8` | 最小最大充電電力値 | Min/max charging electrical power values | 8B | Get | 0 - 999,999,999 W (min:max) |
| `0xC9` | 最小最大放電電力値 | Min/max discharging electrical power values | 8B | Get | 0 - 999,999,999 W (min:max) |
| `0xCF` | 運転動作状態 | Working operation status | 1B | Get | `0x42`=Charging, `0x43`=Discharging, `0x44`=Standby |
| `0xDA` | 運転モード設定 | Operation mode setting | 1B | Set/Get | `0x42`=Charge, `0x43`=Discharge, `0x44`=Standby |
| `0xDB` | 系統連系状態 | System-interconnected type | 1B | Get | `0x00`=Connected, `0x01`=Independent, `0x02`=Cannot accept reverse flow |
| `0xE2` | 蓄電残量1 | Remaining stored electricity1 | 4B | Get | 0 - 999,999,999 Wh |
| `0xE3` | 蓄電残量2 | Remaining stored electricity2 | 2B | Get | 0 - 3276.6 Ah (unit 0.1Ah) |
| `0xE4` | 蓄電残量3 (SoC) | Remaining stored electricity3 | 1B | Get | 0 - 100 (%) |
| `0xE6` | 蓄電池タイプ | Storage battery type | 1B | Get | `0x00`-`0xFF` |

---

## ☀️ 3. Solar Power Generation Object (0x027901)
PV generation monitoring.

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Range / Values |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0x80` | 動作状態 | Operation status | 1B | Get | `0x30`=ON, `0x31`=OFF |
| `0x81` | 設置場所 | Installation location | 1B | Set/Get | Reference standard |
| `0x82` | 規格Version情報 | Standard version information | 4B | Get | `0x00005101` |
| `0x83` | 識別番号 | Identification number | 17B | Get | `0xFE` + `0x000131` + 13 bytes |
| `0x88` | 異常発生状態 | Fault status | 1B | Get | `0x41`=Fault, `0x42`=Normal |
| `0x8A` | メーカコード | Manufacturer code | 3B | Get | `0x000131` |
| `0x97` | 現在時刻設定 | Current time setting | 2B | Set/Get | `0x00`-`0x17`: `0x00`-`0x3B` |
| `0x98` | 現在年月日設定 | Current date setting | 4B | Set/Get | YYYY:MM:DD |
| `0x9D` | 状変アナウンスプロパティマップ | Status change announcement property map | 5B | Get | `0x04808188B1` |
| `0x9E` | Set プロパティマップ | Set property map | 9B | Get | `0x08819798A0A1A2C1E8` |
| `0x9F` | Get プロパティマップ | Get property map | 17B | Get | 27 properties map |
| `0xA0` | 出力制御設定1 | Output power control setting 1 | 1B | Set/Get | 0-100% |
| `0xA1` | 出力制御設定2 | Output power control setting 2 | 2B | Set/Get | 0-5600 W |
| `0xA2` | 余剰買取制御機能 | Surplus power purchasing control function setting | 1B | Set/Get | `0x41`=Enable, `0x42`=Disable |
| `0xB0` | 出力制御スケジュール | Output control schedule | 100B | Get | YYYY:MM:DD + Schedule Data |
| `0xB1` | 次回アクセス日時 | Next access date and time | 7B | Get | YYYY:MM:DD:HH:MM:SS |
| `0xB2` | 余剰買取制御機能設定 | Surplus power purchasing control function type | 1B | Get | `0x41`=Enable, `0x42`=Disable |
| `0xB4` | 上限クリップ設定値 | Maximum output power limit setting | 2B | Get | 0-5600 W |
| `0xC1` | FIT契約タイプ | FIT contract type | 1B | Set/Get | `0x41`=FIT, `0x42`=Non-FIT, `0x43`=None |
| `0xC2` | 自家消費タイプ | Self-consumption type | 1B | Get | `0x41`=Yes, `0x42`=No, `0x43`=Unknown |
| `0xC3` | 設備認定容量 | Equipment certification capacity | 2B | Get | 0-5600 W |
| `0xC4` | 換算係数 | Conversion coefficient | 1B | Get |  |
| `0xD0` | 系統連系状態 | System-interconnected type | 1B | Get | `0x00`=Connected, `0x01`=Independent, `0x02`=Cannot accept reverse flow |
| `0xD1` | 出力抑制状態 | Output power restraint status | 1B | Get | `0x41`=Output power control, `0x42`=Excluding power control |
| `0xE0` | 瞬時発電電力計測値 | Measured instantaneous amount of electricity generated | 2B | Get | 0 - 5600 W |
| `0xE1` | 積算発電電力量計測値 | Measured cumulative amount of electricity generated | 4B | Get | 0.000 - 999,999.999 kWh |
| `0xE8` | 定格発電電力値 | Rated electrical power generation | 2B | Set/Get | 0 - 5600 W |
