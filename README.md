# go-halib

HyperAccel device library for Kubernetes / container tooling — the **shared device-access layer** for the LPU (bertha) stack. NVIDIA **go-nvlib** 패럴.

## 왜 (Why)
`bertha-device-plugin` · `ha-container-toolkit` · `bertha-feature-discovery` 가 **PCI 디스커버리 · CDI 생성 · 디바이스 health 를 각자 복붙**하고 있음 (`pci_manager.go` ↔ `discovery/pci.go`, `pkg/cdi` ↔ `internal/cdi/spec.go`). 이 lib 이 그걸 한 곳으로 모은다.

## 패키지
| 패키지 | 역할 | 출처(port) |
|---|---|---|
| `types` | Device/Stat 공통 타입 (vendor-neutral) | 신규 |
| `driver` | `HWProber` 인터페이스 + 백엔드 | device-plugin `pkg/driver` 리프트 |
| `discovery` | PCI sysfs 스캔 + `/dev/ha` 해석 (+fake) | device-plugin `pkg/device` ∪ ha-ctk `internal/discovery` |
| `cdi` | CDI 스펙 생성 | device-plugin `pkg/cdi` ∪ ha-ctk `internal/cdi` |
| `topology` | NUMA/PCIe 스위치 (예정) | device-plugin `pkg/topology` |

## driver 백엔드 (핵심 설계)
`HWProber` 인터페이스 뒤에서 백엔드를 **갈아끼움** (rewrite 아님):
- **지금**: `HADriver` — CGO `-lha_driver` → `ha_open_device`/`ha_get_device_stat`. 빌드태그 `hyperdriver`.
- **궁극**: `SMIProber` — `ha_smi`(SDK 공식 SMI, `SOFT-2098` 후). 백엔드만 추가, 컨슈머 무변경.
- **noop**: `-tags hyperdriver` 없이 빌드(CI, ha-ctk distroless CGO_ENABLED=0) — health 미수행.
- **mock**: 테스트.

## 소비 (마이그레이션)
1. `device-plugin`: `pkg/{driver,device,cdi,topology}` → go-halib import
2. `ha-ctk`: `internal/{discovery,cdi}` → go-halib import + **driver(health) 신규 획득**
3. `BFD`: discovery + topology 사용

## 빌드
```bash
go build ./...                    # noop 경로 (CGO 불필요, 기본)
go build -tags hyperdriver ./...  # 실HW (ha_driver.h + libha_driver.so 필요)
```

## 네이밍
lib 타입은 **vendor-neutral**(`Device`, bertha 아님). CDI kind/리소스명(`hyperaccel.ai/lpu` vs `/bertha`)은 **컨슈머가 주입** → generic 네이밍 일원화 지점.

## 상태 (2026-06-22)
**lib 5/5 패키지 port 완료** (device-plugin canonical 기준, ~1200줄):
- ✅ `types` · `driver`(HWProber/HADriver real+noop/Mock) · `discovery`(**PCI + fake**, factory) · `topology`(NUMA/PCIe) · `cdi`(kind 파라미터화)
- `go build ./...` + `go vet` 통과 (noop 경로; hyperdriver 경로는 `libha_driver.so`+`ha_driver.h` 필요)
- cdi dep `tags.cncf.io/container-device-interface v0.8.0` 핀 (device-plugin과 동일), 그 외 dep-free(glog→stdlib log)
- fake에서 의도적으로 드롭: ghw 스캔 경로(secondary), health reader(→consumer), k8s allocate(→consumer)

**남은 것:**
- **유닛 테스트 port** — device-plugin `pkg/{driver,device,cdi}/*_test.go` + `topology_test.go` (새 구조에 맞게 조정)
- **consumer 재배선** (★e2e 환경 필요) — device-plugin/ha-ctk가 go-halib import + 자기 복사본 삭제, health 루프는 device-plugin이 `driver.HWProber`+sysfs로 구성, **fake e2e 전·후 동일** 게이트
- `go.mod` go 1.23 → device-plugin(1.25.1)과 정렬

(미발행, 로컬)
