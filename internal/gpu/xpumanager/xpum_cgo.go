package xpumanager

/*
#cgo CFLAGS:  -I/usr/include
#cgo LDFLAGS: -L/usr/lib/x86_64-linux-gnu -lxpum -Wl,-rpath,/usr/lib/x86_64-linux-gnu -Wl,--allow-shlib-undefined

// Override paths via environment before building:
//   CGO_CFLAGS="-I/your/include"  CGO_LDFLAGS="-L/your/lib -lxpum"

#include <stdint.h>
#include <stdbool.h>
#include <string.h>
#include <stdlib.h>

// ── Sizing constants (mirrors xpum_structs.h) ────────────────────────────────
// XPUM_MAX_STR_LENGTH  : max length of string fields in device info structs.
// XPUM_STATS_MAX       : number of metric types == last xpum_stats_type_t + 1.
#define XPUM_MAX_STR_LENGTH 256
#define XPUM_STATS_MAX      40

// ── Primitive typedefs ────────────────────────────────────────────────────────
typedef int32_t xpum_device_id_t;
typedef int32_t xpum_result_t;
#define XPUM_OK 0

// ── Enumerations ──────────────────────────────────────────────────────────────
typedef enum {
    XPUM_DEVICE_TYPE_GPU = 0,
    XPUM_DEVICE_TYPE_CPU = 1,
} xpum_device_type_t;

typedef enum {
    XPUM_DEVICE_FUNCTION_TYPE_PHYSICAL = 0,
    XPUM_DEVICE_FUNCTION_TYPE_VIRTUAL  = 1,
    XPUM_DEVICE_FUNCTION_TYPE_OTHER    = 0xff,
} xpum_device_function_type_t;

typedef enum {
    XPUM_STATS_GPU_UTILIZATION                            = 0,
    XPUM_STATS_EU_ACTIVE                                  = 1,
    XPUM_STATS_EU_STALL                                   = 2,
    XPUM_STATS_EU_IDLE                                    = 3,
    XPUM_STATS_POWER                                      = 4,
    XPUM_STATS_ENERGY                                     = 5,
    XPUM_STATS_GPU_FREQUENCY                              = 6,
    XPUM_STATS_GPU_CORE_TEMPERATURE                       = 7,
    XPUM_STATS_MEMORY_USED                                = 8,
    XPUM_STATS_MEMORY_UTILIZATION                         = 9,
    XPUM_STATS_MEMORY_BANDWIDTH                           = 10,
    XPUM_STATS_MEMORY_READ                                = 11,
    XPUM_STATS_MEMORY_WRITE                               = 12,
    XPUM_STATS_MEMORY_READ_THROUGHPUT                     = 13,
    XPUM_STATS_MEMORY_WRITE_THROUGHPUT                    = 14,
    XPUM_STATS_ENGINE_GROUP_COMPUTE_ALL_UTILIZATION       = 15,
    XPUM_STATS_ENGINE_GROUP_MEDIA_ALL_UTILIZATION         = 16,
    XPUM_STATS_ENGINE_GROUP_COPY_ALL_UTILIZATION          = 17,
    XPUM_STATS_ENGINE_GROUP_RENDER_ALL_UTILIZATION        = 18,
    XPUM_STATS_ENGINE_GROUP_3D_ALL_UTILIZATION            = 19,
    XPUM_STATS_RAS_ERROR_CAT_RESET                        = 20,
    XPUM_STATS_RAS_ERROR_CAT_PROGRAMMING_ERRORS           = 21,
    XPUM_STATS_RAS_ERROR_CAT_DRIVER_ERRORS                = 22,
    XPUM_STATS_RAS_ERROR_CAT_CACHE_ERRORS_CORRECTABLE     = 23,
    XPUM_STATS_RAS_ERROR_CAT_CACHE_ERRORS_UNCORRECTABLE   = 24,
    XPUM_STATS_RAS_ERROR_CAT_DISPLAY_ERRORS_CORRECTABLE   = 25,
    XPUM_STATS_RAS_ERROR_CAT_DISPLAY_ERRORS_UNCORRECTABLE = 26,
    XPUM_STATS_RAS_ERROR_CAT_NON_COMPUTE_ERRORS_CORRECTABLE   = 27,
    XPUM_STATS_RAS_ERROR_CAT_NON_COMPUTE_ERRORS_UNCORRECTABLE = 28,
    XPUM_STATS_GPU_REQUEST_FREQUENCY                      = 29,
    XPUM_STATS_MEMORY_TEMPERATURE                         = 30,
    XPUM_STATS_FREQUENCY_THROTTLE                         = 31,
    XPUM_STATS_PCIE_READ_THROUGHPUT                       = 32,
    XPUM_STATS_PCIE_WRITE_THROUGHPUT                      = 33,
    XPUM_STATS_PCIE_READ                                  = 34,
    XPUM_STATS_PCIE_WRITE                                 = 35,
    XPUM_STATS_ENGINE_UTILIZATION                         = 36,
    XPUM_STATS_FABRIC_THROUGHPUT                          = 37,
    XPUM_STATS_FREQUENCY_THROTTLE_REASON_GPU              = 38,
    XPUM_STATS_MEDIA_ENGINE_FREQUENCY                     = 39,
} xpum_stats_type_t;

typedef enum {
    XPUM_ENGINE_TYPE_COMPUTE           = 0,
    XPUM_ENGINE_TYPE_RENDER            = 1,
    XPUM_ENGINE_TYPE_DECODE            = 2,
    XPUM_ENGINE_TYPE_ENCODE            = 3,
    XPUM_ENGINE_TYPE_COPY              = 4,
    XPUM_ENGINE_TYPE_MEDIA_ENHANCEMENT = 5,
    XPUM_ENGINE_TYPE_3D                = 6,
    XPUM_ENGINE_TYPE_UNKNOWN           = 7,
} xpum_engine_type_t;

typedef enum {
    XPUM_FABRIC_THROUGHPUT_TYPE_RECEIVED            = 0,
    XPUM_FABRIC_THROUGHPUT_TYPE_TRANSMITTED         = 1,
    XPUM_FABRIC_THROUGHPUT_TYPE_RECEIVED_COUNTER    = 2,
    XPUM_FABRIC_THROUGHPUT_TYPE_TRANSMITTED_COUNTER = 3,
} xpum_fabric_throughput_type_t;

// ── Structures ────────────────────────────────────────────────────────────────

typedef struct {
    xpum_device_id_t            deviceId;
    xpum_device_type_t          type;
    xpum_device_function_type_t functionType;
    char uuid[XPUM_MAX_STR_LENGTH];
    char deviceName[XPUM_MAX_STR_LENGTH];
    char PCIDeviceId[XPUM_MAX_STR_LENGTH];
    char PCIBDFAddress[XPUM_MAX_STR_LENGTH];
    char VendorName[XPUM_MAX_STR_LENGTH];
    char drmDevice[XPUM_MAX_STR_LENGTH];
} xpum_device_basic_info;

typedef struct {
    xpum_stats_type_t metricsType;
    bool              isCounter;
    uint64_t          value;       // current reading; divide by scale for real units
    uint64_t          accumulated; // cumulative total; used when isCounter==true
    uint64_t          min;
    uint64_t          avg;
    uint64_t          max;
    uint32_t          scale;       // actual = value / scale
} xpum_device_stats_data_t;

typedef struct {
    xpum_device_id_t         deviceId;
    bool                     isTileData; // false = device-level aggregated
    int32_t                  tileId;
    int32_t                  count;      // number of valid entries in dataList
    xpum_device_stats_data_t dataList[XPUM_STATS_MAX];
} xpum_device_stats_t;

typedef struct {
    bool               isTileData;
    int32_t            tileId;
    uint64_t           index;       // engine index within its type
    xpum_engine_type_t type;        // "type" is a Go keyword – access via helper below
    uint64_t           value;
    uint64_t           min;
    uint64_t           avg;
    uint64_t           max;
    uint32_t           scale;
    xpum_device_id_t   deviceId;
} xpum_device_engine_stats_t;

typedef struct {
    uint32_t                      tile_id;
    uint32_t                      remote_device_id;
    uint32_t                      remote_device_tile_id;
    xpum_fabric_throughput_type_t type;   // also a Go keyword – access via helper
    uint64_t                      value;
    uint64_t                      accumulated;
    uint64_t                      min;
    uint64_t                      avg;
    uint64_t                      max;
    uint32_t                      scale;
    xpum_device_id_t              deviceId;
} xpum_device_fabric_throughput_stats_t;

// ── xpumanager function declarations ─────────────────────────────────────────
// The library is compiled as C++ with extern "C" linkage; we redeclare in C.
// xpumInit has a C++ default arg (= false); we always supply it explicitly.
extern xpum_result_t xpumInit(bool zeinitDisable);
extern xpum_result_t xpumShutdown(void);

extern xpum_result_t xpumGetDeviceList(
    xpum_device_basic_info deviceList[], int *count);

extern xpum_result_t xpumGetStats(
    xpum_device_id_t    deviceId,
    xpum_device_stats_t dataList[],
    uint32_t           *count,
    uint64_t           *begin,
    uint64_t           *end,
    uint64_t            sessionId);

extern xpum_result_t xpumGetEngineStats(
    xpum_device_id_t           deviceId,
    xpum_device_engine_stats_t dataList[],
    uint32_t                  *count,
    uint64_t                  *begin,
    uint64_t                  *end,
    uint64_t                   sessionId);

extern xpum_result_t xpumGetFabricThroughputStats(
    xpum_device_id_t                      deviceId,
    xpum_device_fabric_throughput_stats_t dataList[],
    uint32_t                             *count,
    uint64_t                             *begin,
    uint64_t                             *end,
    uint64_t                              sessionId);

// ── Inline helpers ────────────────────────────────────────────────────────────

// Apply scale divisor; guard against zero scale.
static inline double xpum_scaled(uint64_t raw, uint32_t scale) {
    return (scale > 0) ? ((double)raw / (double)scale) : (double)raw;
}
static inline double xpum_scaled_acc(uint64_t acc, uint32_t scale) {
    return (scale > 0) ? ((double)acc / (double)scale) : (double)acc;
}

// Accessors for struct fields named "type" (reserved keyword in Go).
static inline xpum_engine_type_t xpum_engine_get_type(
        const xpum_device_engine_stats_t *e) {
    return e->type;
}
static inline xpum_fabric_throughput_type_t xpum_fabric_get_type(
        const xpum_device_fabric_throughput_stats_t *f) {
    return f->type;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Buffer sizes for pre-allocated stack arrays passed into XPUM calls.
const (
	maxDevicesCap     = 32  // max GPUs in a single system
	maxStatEntriesCap = 16  // per device: 1 device entry + up to ~15 tiles
	maxEnginesCap     = 128 // per device: generous for multi-tile (e.g. PVC has many)
	maxFabricCap      = 64  // per device: Xe-Link fabric ports
)

// ── Go-level result types ─────────────────────────────────────────────────────

type cDeviceBasicInfo struct {
	DeviceID   int32
	UUID       string
	DeviceName string
	PCIAddress string
}

// cStatEntry holds one decoded metric from xpum_device_stats_data_t.
type cStatEntry struct {
	MetricType  int32   // xpum_stats_type_t value
	IsCounter   bool    // true → use Accumulated; false → use Value
	Value       float64 // value / scale
	Accumulated float64 // accumulated / scale
}

// cEngineEntry holds one decoded engine utilisation entry.
type cEngineEntry struct {
	EngineType  int32   // xpum_engine_type_t value
	EngineIndex int64   // engine index within its type
	Value       float64 // warmup-window mean utilisation % (= RawValue / RawScale)
	RawValue    uint64  // raw avg from XPUM over the warmup window — for diagnostics
	RawCur      uint64  // raw value of the last ~100 ms snapshot — for diagnostics only
	RawScale    uint32  // scale divisor reported by XPUM — for diagnostics
}

// cFabricEntry holds one decoded Xe-Link throughput entry.
type cFabricEntry struct {
	FabricType int32   // xpum_fabric_throughput_type_t value
	Value      float64 // kB/s
}

// ── Public C stat type constants (for use in provider.go) ─────────────────────

const (
	statGPUUtilization   = int32(C.XPUM_STATS_GPU_UTILIZATION)
	statEUActive         = int32(C.XPUM_STATS_EU_ACTIVE)
	statEUStall          = int32(C.XPUM_STATS_EU_STALL)
	statEUIdle           = int32(C.XPUM_STATS_EU_IDLE)
	statPower            = int32(C.XPUM_STATS_POWER)
	statGPUFrequency     = int32(C.XPUM_STATS_GPU_FREQUENCY)
	statCoreTempC        = int32(C.XPUM_STATS_GPU_CORE_TEMPERATURE)
	statMemUsed          = int32(C.XPUM_STATS_MEMORY_USED)
	statMemUtil          = int32(C.XPUM_STATS_MEMORY_UTILIZATION)
	statMemBandwidth     = int32(C.XPUM_STATS_MEMORY_BANDWIDTH)
	statMemReadTP        = int32(C.XPUM_STATS_MEMORY_READ_THROUGHPUT)
	statMemWriteTP       = int32(C.XPUM_STATS_MEMORY_WRITE_THROUGHPUT)
	statRasReset         = int32(C.XPUM_STATS_RAS_ERROR_CAT_RESET)
	statRasProgramming   = int32(C.XPUM_STATS_RAS_ERROR_CAT_PROGRAMMING_ERRORS)
	statRasDriver        = int32(C.XPUM_STATS_RAS_ERROR_CAT_DRIVER_ERRORS)
	statRasCacheCorr     = int32(C.XPUM_STATS_RAS_ERROR_CAT_CACHE_ERRORS_CORRECTABLE)
	statRasCacheUncorr   = int32(C.XPUM_STATS_RAS_ERROR_CAT_CACHE_ERRORS_UNCORRECTABLE)
	statRasNonCompCorr   = int32(C.XPUM_STATS_RAS_ERROR_CAT_NON_COMPUTE_ERRORS_CORRECTABLE)
	statRasNonCompUncorr = int32(C.XPUM_STATS_RAS_ERROR_CAT_NON_COMPUTE_ERRORS_UNCORRECTABLE)
	statMemTempC         = int32(C.XPUM_STATS_MEMORY_TEMPERATURE)
	statMediaFrequency   = int32(C.XPUM_STATS_MEDIA_ENGINE_FREQUENCY)

	// Engine-group aggregate utilisation — available from xpumGetStats (not xpumGetEngineStats).
	// Used as fallback when xpumGetEngineStats is unsupported by the driver/GPU.
	statEngineGroupCompute = int32(C.XPUM_STATS_ENGINE_GROUP_COMPUTE_ALL_UTILIZATION)
	statEngineGroupMedia   = int32(C.XPUM_STATS_ENGINE_GROUP_MEDIA_ALL_UTILIZATION)
	statEngineGroupCopy    = int32(C.XPUM_STATS_ENGINE_GROUP_COPY_ALL_UTILIZATION)
	statEngineGroupRender  = int32(C.XPUM_STATS_ENGINE_GROUP_RENDER_ALL_UTILIZATION)

	engineTypeCompute = int32(C.XPUM_ENGINE_TYPE_COMPUTE)
	engineTypeRender  = int32(C.XPUM_ENGINE_TYPE_RENDER)
	engineTypeDecode  = int32(C.XPUM_ENGINE_TYPE_DECODE)
	engineTypeEncode  = int32(C.XPUM_ENGINE_TYPE_ENCODE)
	engineTypeCopy    = int32(C.XPUM_ENGINE_TYPE_COPY)
	engineTypeMediaEM = int32(C.XPUM_ENGINE_TYPE_MEDIA_ENHANCEMENT)
	engineType3D      = int32(C.XPUM_ENGINE_TYPE_3D)

	fabricTypeReceived    = int32(C.XPUM_FABRIC_THROUGHPUT_TYPE_RECEIVED)
	fabricTypeTransmitted = int32(C.XPUM_FABRIC_THROUGHPUT_TYPE_TRANSMITTED)
)

// ── Bridge functions ──────────────────────────────────────────────────────────

// cInit calls xpumInit(false).  Must be called once before any other cXxx fn.
func cInit() error {
	if rc := C.xpumInit(C.bool(false)); rc != C.XPUM_OK {
		return fmt.Errorf("xpumInit returned %d", int(rc))
	}
	return nil
}

// cShutdown calls xpumShutdown.
func cShutdown() { C.xpumShutdown() }

// cGetDeviceList enumerates all XPUM-managed GPU devices.
func cGetDeviceList() ([]cDeviceBasicInfo, error) {
	var buf [maxDevicesCap]C.xpum_device_basic_info
	count := C.int(maxDevicesCap)

	if rc := C.xpumGetDeviceList(&buf[0], &count); rc != C.XPUM_OK {
		return nil, fmt.Errorf("xpumGetDeviceList rc=%d", int(rc))
	}

	n := int(count)
	out := make([]cDeviceBasicInfo, 0, n)
	for i := 0; i < n; i++ {
		d := buf[i]
		out = append(out, cDeviceBasicInfo{
			DeviceID:   int32(d.deviceId),
			UUID:       C.GoString((*C.char)(unsafe.Pointer(&d.uuid[0]))),
			DeviceName: C.GoString((*C.char)(unsafe.Pointer(&d.deviceName[0]))),
			PCIAddress: C.GoString((*C.char)(unsafe.Pointer(&d.PCIBDFAddress[0]))),
		})
	}
	return out, nil
}

// cGetStats returns device-level (non-tile) stat entries for deviceID.
//
// sessionID is the XPUM session identifier used for delta tracking.
// Always pass 0: it is the only session ID that survives xpumShutdown/
// xpumInit cycles.  The caller must issue a baseline call (results discarded)
// before the warmup sleep to re-anchor the delta window start; see collect().
//
// beginNs and endNs are the XPUM-reported measurement window boundaries
// (raw uint64 units from the driver — useful for diagnostics / --debug logs).
func cGetStats(deviceID int32, sessionID uint64) (entries []cStatEntry, beginNs uint64, endNs uint64, err error) {
	var buf [maxStatEntriesCap]C.xpum_device_stats_t
	count := C.uint32_t(maxStatEntriesCap)
	var begin, end C.uint64_t

	if rc := C.xpumGetStats(
		C.xpum_device_id_t(deviceID),
		&buf[0], &count,
		&begin, &end,
		C.uint64_t(sessionID),
	); rc != C.XPUM_OK {
		return nil, 0, 0, fmt.Errorf("xpumGetStats device=%d rc=%d", deviceID, int(rc))
	}

	var out []cStatEntry
	for i := 0; i < int(count); i++ {
		s := &buf[i]
		if bool(s.isTileData) {
			continue // aggregate device-level data only
		}
		nMetrics := int(s.count)
		if nMetrics > int(C.XPUM_STATS_MAX) {
			nMetrics = int(C.XPUM_STATS_MAX)
		}
		for j := 0; j < nMetrics; j++ {
			m := s.dataList[j]
			out = append(out, cStatEntry{
				MetricType:  int32(m.metricsType),
				IsCounter:   bool(m.isCounter),
				Value:       float64(C.xpum_scaled(m.value, m.scale)),
				Accumulated: float64(C.xpum_scaled_acc(m.accumulated, m.scale)),
			})
		}
	}
	return out, uint64(begin), uint64(end), nil
}

// cGetEngineStats returns per-engine (non-tile) utilisation for deviceID.
// sessionID follows the same convention as cGetStats.
// beginNs and endNs are the XPUM-reported measurement window boundaries —
// returned here (rather than discarded) so callers can log the actual window
// and verify whether the two-pass session anchoring is working as expected.
func cGetEngineStats(deviceID int32, sessionID uint64) (entries []cEngineEntry, beginNs uint64, endNs uint64, err error) {
	var buf [maxEnginesCap]C.xpum_device_engine_stats_t
	count := C.uint32_t(maxEnginesCap)
	var begin, end C.uint64_t

	if rc := C.xpumGetEngineStats(
		C.xpum_device_id_t(deviceID),
		&buf[0], &count,
		&begin, &end,
		C.uint64_t(sessionID),
	); rc != C.XPUM_OK {
		return nil, 0, 0, fmt.Errorf("xpumGetEngineStats device=%d rc=%d", deviceID, int(rc))
	}

	var out []cEngineEntry
	for i := 0; i < int(count); i++ {
		e := &buf[i]
		if bool(e.isTileData) {
			continue
		}
		out = append(out, cEngineEntry{
			EngineType:  int32(C.xpum_engine_get_type(e)),
			EngineIndex: int64(e.index),
			// Use e.avg (arithmetic mean across the ~14 warmup-window samples) rather
			// than e.value (last ~100 ms snapshot).  For bursty media workloads the
			// GPU may be idle in the last 100 ms even under steady load, causing e.value
			// to be 0 while e.avg correctly reflects the sustained utilisation.
			Value:    float64(C.xpum_scaled(e.avg, e.scale)),
			RawValue: uint64(e.avg),
			RawCur:   uint64(e.value), // last ~100 ms snapshot — kept for --debug logs
			RawScale: uint32(e.scale),
		})
	}
	return out, uint64(begin), uint64(end), nil
}

// cGetFabricStats returns Xe-Link throughput entries for deviceID.
// Returns an empty slice (no error) if Xe-Link is not available on the device.
// sessionID follows the same convention as cGetStats.
func cGetFabricStats(deviceID int32, sessionID uint64) ([]cFabricEntry, error) {
	var buf [maxFabricCap]C.xpum_device_fabric_throughput_stats_t
	count := C.uint32_t(maxFabricCap)
	var begin, end C.uint64_t

	rc := C.xpumGetFabricThroughputStats(
		C.xpum_device_id_t(deviceID),
		&buf[0], &count,
		&begin, &end,
		C.uint64_t(sessionID),
	)
	// XPUM may return a non-OK code when Xe-Link is absent; treat as empty.
	if rc != C.XPUM_OK {
		return nil, nil //nolint:nilerr
	}

	var out []cFabricEntry
	for i := 0; i < int(count); i++ {
		f := &buf[i]
		ft := int32(C.xpum_fabric_get_type(f))
		if ft != fabricTypeReceived && ft != fabricTypeTransmitted {
			continue // skip counter variants; we only want instantaneous rates
		}
		out = append(out, cFabricEntry{
			FabricType: ft,
			Value:      float64(C.xpum_scaled(f.value, f.scale)),
		})
	}
	return out, nil
}
