# Daikin Ecocute ECHONET Lite Specification Reference
**Adapter Model:** LZ-000097 (HP Wireless Adapter)
**Certification:** HP-HP-A3-2 (HP Heat Pump Water Heater, 2nd edition)
**Manufacturer:** Daikin Industries (0x000008)
**Protocol:** ECHONET Lite Application Communication Interface Ver. 1.1
**Class:** Electric Water Heater (電気温水器クラス) 0x026B

This document is consolidated from the official ECHONET certification property declaration sheet (300 DPI PNG verification).

---

## 1. General / Standard Properties

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Values / Notes |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0x80` | 動作状態 | Operation status | 1B | Get | `0x30` (fixed, always ON) |
| `0x81` | 設置場所 | Installation location | 1 or 17B ※1 | Set/Get | `0x00`-`0xFF` |
| `0x82` | 規格Version情報 | Standard version information | 4B | Get | `0x00 0x00 0x4A 0x00` |
| `0x83` | 識別番号 | Identification number | 9 or 17B | Get | Optional. `0xFE` + manufacturer code (`0x00 0x00 0x08`) + adapter MAC (6B) + zero padding |
| `0x86` | メーカ異常コード | Manufacturer fault code | max 225B | Get | Optional. Returns "not supported" ※2 |
| `0x88` | 異常発生状態 | Fault status | 1B | Get | `0x41`=Fault, `0x42`=Normal |
| `0x89` | 異常内容 | Fault description | 2B | Get | `0x0000`-`0xFFFF`. Optional ※2 |
| `0x8A` | メーカコード | Manufacturer code | 3B | Get | `0x00 0x00 0x08` (Daikin, fixed) |
| `0x93` | 遠隔操作設定 | Remote control setting | 1B | Set/Get | Optional. `0x41`, `0x42` |
| `0x9D` | 状変アナウンスプロパティマップ | Status change announcement property map | max 17B | Get | Varies by connected water heater |
| `0x9E` | Setプロパティマップ | Set property map | max 17B | Get | Varies by connected water heater |
| `0x9F` | Getプロパティマップ | Get property map | max 17B | Get | Varies by connected water heater |

---

## 2. Electric Water Heater Class Properties (0x026B)

### Core Operation

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Values / Notes |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0xB0` | 沸き上げ自動設定 | Automatic water heating setting | 1B | Set/Get | `0x41`=Automatic, `0x42`=Manual, `0x43`=Stop |
| `0xB2` | 沸き上げ中状態 | Water heating status | 1B | Get | `0x41`=Heating, `0x42`=Not heating |
| `0xC0` | 昼間沸き増し許可設定 | Daytime reheating permission setting | 1B | Set/Get | `0x41`=Permitted, `0x42`=Not permitted |
| `0xC3` | 給湯中状態 | Hot water supply status | 1B | Get | `0x41`=Supplying, `0x42`=Not supplying |
| `0xE3` | 風呂自動モード設定 | Automatic bath mode setting | 1B | Set/Get | `0x41`=ON, `0x42`=OFF. Bath-only units (給専) return "not supported" |

### Energy Shift Properties

These properties manage the energy shift (demand response) scheduling for off-peak water heating.

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Values / Notes |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0xC7` | エネルギーシフト参加状態 | Energy shift participation status | 1B | Set/Get | `0x00`=Not participating, `0x01`=Participating |
| `0xC8` | 沸き上げ開始基準時刻 | Standard time to start heating | 1B | Get | `0x14`=20:00, `0x15`=21:00, `0x16`=22:00, `0x17`=23:00, `0x18`=24:00, `0x01`=01:00 |
| `0xC9` | エネルギーシフト回数 | Number of energy shifts | 1B | Get | `0x01` (this adapter supports 1 shift) |
| `0xCA` | 昼間沸き上げシフト時刻1 | Daytime heating shift time 1 | 1B | Set/Get | `0x00`=Cleared, `0x09`=09:00, `0x0A`=10:00, `0x0B`=11:00, `0x0C`=12:00, `0x0D`=13:00, `0x0E`=14:00, `0x0F`=15:00, `0x10`=16:00, `0x11`=17:00 |
| `0xCB` | 沸き上げ予測電力量1 | Predicted heating power for shift 1 | 16B | Get | Compound: `0x00000000`-`0xFFFFFFFD` (4 x 4-byte Wh values) |
| `0xCC` | 時間当たり消費電力量1 | Hourly power consumption 1 | 8B | Get | Compound: `0x0000`-`0xFFFD` (4 x 2-byte W values) |
| `0xCD` | 昼間沸き上げシフト時刻2 | Daytime heating shift time 2 | 1B | Set/Get | Returns "not supported". ※3 |
| `0xCE` | 沸き上げ予測電力量2 | Predicted heating power for shift 2 | 12B | Get | Returns "not supported". Compound. ※3 |
| `0xCF` | 時間当たり消費電力量2 | Hourly power consumption 2 | 6B | Get | Returns "not supported". Compound. ※3 |

---

## Notes

- **※1** — 17-byte variant of installation location property is optional.
- **※2** — If manufacturer fault code (0x86) is present, fault description (0x89) is mandatory. Fault description alone is allowed.
- **※3** — Energy shift 2 properties (0xCD, 0xCE, 0xCF) are mandatory only for models supporting 2 energy shift sessions. This adapter supports 1 shift (0xC9=0x01), so shift-2 properties return "not supported".
- **Response time:** All properties respond within 20 seconds (20秒未満).
- **Connected water heater variability:** Property maps (0x9D, 0x9E, 0x9F) and some values vary depending on the connected water heater unit.
- **Bath-only units (給専):** Automatic bath mode (0xE3) returns "not supported" on bath-only models.
