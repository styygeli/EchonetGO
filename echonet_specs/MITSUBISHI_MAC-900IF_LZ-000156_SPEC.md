# Mitsubishi MAC-900IF ECHONET Lite Specification Reference
**Adapter Model:** MAC-900IF (ECHONET Lite Adapter for Mitsubishi Air Conditioners)
**Certification:** AC-AC-A3-2 (Home Air Conditioner, 2nd edition)
**AIF Registration:** LZ-000156
**Manufacturer:** Mitsubishi Electric (0x000006)
**Protocol:** ECHONET Lite Application Communication Interface Ver. 1.0x
**Class:** Home Air Conditioner (家庭用エアコンクラス) 0x0130
**Appendix:** Release K

This document is consolidated from the official ECHONET certification property declaration sheet (300 DPI PNG verification).

---

## 1. General / Standard Properties

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Values / Notes |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0x80` | 動作状態 | Operation status | 1B | Set/Get | `0x30`=ON, `0x31`=OFF |
| `0x81` | 設置場所 | Installation location | 1 or 17B ※1 | Set/Get | Per ECHONET Appendix |
| `0x82` | 規格Version情報 | Standard version information | 4B | Get | `0x00 0x00 0x4B 0x00` (Appendix K) |
| `0x88` | 異常発生状態 | Fault status | 1B | Get | `0x41`=Fault, `0x42`=Normal |
| `0x8A` | メーカコード | Manufacturer code | 3B | Get | `0x000006` (Mitsubishi Electric) |
| `0x9D` | 状変アナウンスプロパティマップ | Status change announcement property map | max 17B | Get | 6 properties: `0x80 0x81 0x88 0x8F 0xA0 0xB6` |
| `0x9E` | Setプロパティマップ | Set property map | max 17B | Get | 12 properties: `0x80 0x81 0x8F 0x93 0xA0 0xA1 0xA3 0xA4 0xA5 0xB0 0xB3 0xD0` |
| `0x9F` | Getプロパティマップ | Get property map | max 17B | Get | 24 properties (see decoded list below) |

### Decoded Get Property Map (0x9F) — 24 readable EPCs

```
0x80 0x81 0x82 0x83 0x85 0x86 0x88 0x89
0x8A 0x8B 0x8F 0x93 0x9D 0x9E 0x9F 0xA0
0xA1 0xA3 0xA4 0xA5 0xB0 0xB3 0xBB 0xD0
```

---

## 2. Optional Standard Properties

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Values / Notes |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0x83` | 識別番号 | Identification number | 9 or 17B | Get | `0xFE` + manufacturer code (`0x000006`, 3B) + device-specific (13B) |
| `0x86` | メーカ異常コード | Manufacturer fault code | max 225B | Get | `0x06` + `0x000006` + device-specific (fixed length). ※2 |
| `0x89` | 異常内容 | Fault description | 2B | Get | Per ECHONET Appendix fault content definitions. ※2 |

---

## 3. Home Air Conditioner Class Properties (0x0130)

### Explicitly Declared Properties

| EPC | Property Name (JP) | Property Name (EN) | Size | Access | Values / Notes |
| :--- | :--- | :--- | :---: | :---: | :--- |
| `0x8F` | 節電動作設定 | Power saving operation setting | 1B | Set/Get | `0x41`=Power saving, `0x42`=Normal operation |
| `0x93` | 遠隔操作設定 | Remote control setting | 1B | Set (Get unsupported) | `0x41`=Not through public network, `0x42`=Through public network. Get returns "not supported" |
| `0xB0` | 運転モード設定 | Operation mode setting | 1B | Set/Get | `0x41`=Auto, `0x42`=Cool, `0x43`=Heat, `0x44`=Dry, `0x45`=Fan, `0x40`=Other |
| `0xB3` | 温度設定値 | Temperature setting | 1B | Set/Get | `0x00`-`0x32` (0-50 °C) |
| `0xBB` | 室内温度計測値 | Indoor temperature measurement | **1B** | Get | `0x81`-`0x7D` (signed, -127 to +125 °C, 1 °C resolution) |
| `0xA0` | 風量設定 | Air flow rate setting | 1B | Set/Get | `0x41`=Auto, `0x31`-`0x38`=Level 1-8 |

### Additional Readable Properties (from Get map, standard Appendix definitions)

These EPCs appear in the Get property map (0x9F) but are not explicitly declared with device-specific values in this certification sheet. They follow standard ECHONET Appendix K definitions:

| EPC | Property Name (EN) | Notes |
| :--- | :--- | :--- |
| `0x85` | Cumulative operating time | Standard |
| `0x8B` | Business facility code | Standard |
| `0xA1` | Auto air flow direction setting | Also in Set map |
| `0xA3` | Air flow swing setting | Also in Set map |
| `0xA4` | Air flow direction (horizontal) | Also in Set map |
| `0xA5` | Air flow direction (vertical) | Also in Set map |
| `0xD0` | Special state / thermostat state | Also in Set map |

---

## Important: Indoor Temperature Size

**0xBB is 1 byte (signed) on the MAC-900IF**, not 2 bytes. This differs from some other AC implementations that use 2 bytes with 0.1 °C resolution. Our current `etc/specs/home_ac.yaml` has `size: 2` and `scale: 0.1` for 0xBB which would be incorrect for this adapter.

---

## Notes

- **※1** — 17-byte variant of installation location property is optional.
- **※2** — If manufacturer fault code (0x86) is present, fault description (0x89) is mandatory. Fault description alone is allowed.
- **Response time:** All properties respond within 20 seconds (20秒未満).
- **Remote control (0x93):** Set is supported but Get returns "not supported" per the notes column.
- **Compatible models:** Covers a wide range of Mitsubishi MSZ-series wall-mounted and MLZ/MTZ/MBZ/MFZ cassette/floor units from 2020-2023 model years.
