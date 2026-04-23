package dump

import (
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gookit/color"
	"github.com/gookit/goutil/strutil"
)

// printValue must keep track of already-printed pointer values to avoid
// infinite recursion. refer the pkg: github.com/kr/pretty
type visit struct {
	v   uintptr
	typ reflect.Type
}

// Dumper struct definition
type Dumper struct {
	*Options
	// locker for visited
	mu sync.RWMutex
	// visited struct records
	visited map[visit]int
	// is value in the slice, map, struct. will not apply indent.
	msValue bool
	// current depth
	curDepth int
	// current indent string bytes
	indentBytes []byte
	// prevDepth, nextDepth int
	// indentStr, indentPrev, lineEnd string
}

// NewDumper create
func NewDumper(out io.Writer, skip int) *Dumper {
	return &Dumper{
		Options: NewDefaultOptions(out, skip),
		// init map
		visited: make(map[visit]int),
	}
}

// NewWithOptions create
func NewWithOptions(fns ...OptionFunc) *Dumper {
	return NewDumper(os.Stdout, defaultSkip).WithOptions(fns...)
}

// WithSkip for dumper
func (d *Dumper) WithSkip(skip int) *Dumper {
	d.CallerSkip = skip
	return d
}

// WithoutColor for dumper
func (d *Dumper) WithoutColor() *Dumper {
	d.NoColor = true
	return d
}

// WithOptions for dumper
func (d *Dumper) WithOptions(fns ...OptionFunc) *Dumper {
	for _, fn := range fns {
		fn(d.Options)
	}
	return d
}

// ResetOptions for dumper
func (d *Dumper) ResetOptions() {
	d.curDepth = 0
	d.visited = make(map[visit]int)
	d.Options = NewDefaultOptions(os.Stdout, d.CallerSkip)
}

// Dump vars
func (d *Dumper) Dump(vs ...any) { d.dumpVars(vs...) }

// Print vars. alias of Dump()
func (d *Dumper) Print(vs ...any) { d.dumpVars(vs...) }

// Println vars. alias of Dump()
func (d *Dumper) Println(vs ...any) { d.dumpVars(vs...) }

// Fprint print vars to io.Writer
func (d *Dumper) Fprint(w io.Writer, vs ...any) {
	backup := d.Output // backup

	d.Output = w
	d.dumpVars(vs...)
	d.Output = backup // restore
}

// dumpVars go vars
func (d *Dumper) dumpVars(vs ...any) {
	// reset some settings.
	d.curDepth = 0
	d.visited = make(map[visit]int)

	// clear all theme settings.
	if d.NoColor {
		d.ColorTheme = make(Theme)
	}

	// show print position
	if d.ShowFlag != Fnopos {
		// get the print position
		pc, file, line, ok := runtime.Caller(d.CallerSkip)
		if ok {
			d.printCaller(pc, file, line)
		}
	}

	// print var data
	for _, v := range vs {
		// d.advance(1)
		d.printOne(v)
		// d.advance(-1)
	}
}

func (d *Dumper) printCaller(pc uintptr, file string, line int) {
	// eg: github.com/gookit/goutil/dump.ExamplePrint
	fnName := runtime.FuncForPC(pc).Name()

	lineS := strconv.Itoa(line)
	nodes := []string{"PRINT AT "}

	// eg:
	// "PRINT AT github.com/gookit/goutil/dump.ExamplePrint(goutil/dump/dump_test.go:23)"
	// "PRINT AT github.com/gookit/goutil/dump.ExamplePrint(dump_test.go:23)"
	// "PRINT AT github.com/gookit/goutil/dump.ExamplePrint(:23)"
	for _, flag := range callerFlags {
		// has flag
		if d.ShowFlag&flag == 0 {
			continue
		}
		switch flag {
		case Ffunc: // full func name
			nodes = append(nodes, fnName, "(")
		case Ffile: // full file path
			nodes = append(nodes, file)
		case Ffname: // only file name
			fName := path.Base(file) // file name
			nodes = append(nodes, fName)
		default: // Fline
			nodes = append(nodes, ":", lineS)
		}
	}

	// fallback. eg: "PRINT AT goutil/dump/dump_test.go:23"
	if len(nodes) == 1 {
		nodes = append(nodes, file, ":", lineS)
	} else if d.ShowFlag&Ffunc != 0 { // has func, add ")"
		nodes = append(nodes, ")")
	}

	text := strings.Join(nodes, "")
	d.writeOutput(d.ColorTheme.caller(text), "\n")
}

func (d *Dumper) advance(step int) {
	d.curDepth += step
	// d.nextDepth = d.curDepth + step
	if d.curDepth < 1 {
		d.indentBytes = []byte{}
		return
	}

	d.indentBytes = strutil.RepeatBytes(d.IndentChar, d.IndentLen*d.curDepth)
}

func (d *Dumper) printOne(v any) {
	if v == nil {
		d.indentPrint("<nil>,\n")
		return
	}

	if bts, ok := v.([]byte); ok && d.BytesAsString {
		strVal := d.ColorTheme.string(string(bts))
		lenTip := d.ColorTheme.valTip("#len=" + strconv.Itoa(len(bts)) + ",cap=" + strconv.Itoa(cap(bts)))
		d.printf("[]byte(\"%s\"), %s\n", strVal, lenTip)
		return
	}

	// print reflect value
	rv := reflect.ValueOf(v)
	d.printRValue(rv.Type(), rv)
}

// print reflect value
func (d *Dumper) printRValue(t reflect.Type, v reflect.Value) {
	// if is a ptr, get real type and value
	isPtr := t.Kind() == reflect.Ptr
	if isPtr {
		if v.IsNil() {
			d.printf("%s<nil>,\n", t.String())
			return
		}

		v, t = v.Elem(), t.Elem()
		d.indentPrint("&") // add prefix
	}

	if !v.IsValid() {
		d.indentPrint(t.String(), "<nil>, #invalid\n")
	}

	// if v.CanAddr() && !d.checkCyclicRef(t, v) {
	// 	return // don't print v again
	// }

	if d.curDepth > d.MaxDepth {
		// if !v.CanInterface() {
		// 	d.printf("%s,\n", v.String())
		// } else {
		// 	// v.Interface() will stack overflow on cyclic refer
		// 	d.printf("%#v,\n", v.Interface())
		// }
		d.printf("%s(!OVER MAX DEPTH!),\n", v.String())
		return
	}

	switch t.Kind() {
	case reflect.Bool:
		d.printBoolValue(t, v)
	case reflect.Float32, reflect.Float64:
		d.printFloatValue(t, v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		d.printIntValue(t, v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		d.printUintValue(t, v)
	case reflect.String:
		d.printStringValue(t, v)
	case reflect.Complex64, reflect.Complex128:
		d.printComplexValue(t, v)
	case reflect.Slice, reflect.Array:
		d.printSliceOrArrayValue(t, v, isPtr)
	case reflect.Struct:
		d.printStructValue(t, v, isPtr)
	case reflect.Map:
		d.printMapValue(t, v, isPtr)
	case reflect.Interface:
		d.printInterfaceValue(t, v)
	case reflect.Chan:
		d.printChanValue(t, v)
	case reflect.Func:
		d.printFuncValue(t, v)
	case reflect.UnsafePointer:
		d.printUnsafePointerValue(t, v)
	case reflect.Invalid:
		d.printInvalidValue(t, v)
	default:
		d.printDefaultValue(t, v)
	}
}

func (d *Dumper) checkCyclicRef(t reflect.Type, v reflect.Value) (goon bool) {
	addr := v.UnsafeAddr()
	vis := visit{addr, t}

	d.mu.RLock()
	if vd, ok := d.visited[vis]; ok && vd < d.MaxDepth {
		d.indentPrint(t.String(), "{(!CYCLIC REFERENCE!)}\n")
		d.mu.RUnlock()
		return false // don't print v again
	}
	d.mu.RUnlock()

	// record visited
	d.mu.Lock()
	d.visited[vis] = d.curDepth
	d.mu.Unlock()
	return true
}

func (d *Dumper) rvStringer(rt reflect.Type, rv reflect.Value) string {
	// fmt.Println("Implements fmt.Stringer:", t.Implements(stringerType))
	if rv.CanInterface() && rt.Implements(stringerType) {
		return d.ColorTheme.valTip(` #str: "` + rv.Interface().(fmt.Stringer).String() + `"`)
	}
	return ""
}

func (d *Dumper) writeOutput(v ...any) {
	if d.NoColor {
		_, _ = fmt.Fprint(d.Output, v...)
	} else {
		color.Fprint(d.Output, v...)
	}
}

func (d *Dumper) printf(f string, v ...any) {
	if !d.msValue {
		_, _ = d.Output.Write(d.indentBytes)
	}

	if d.NoColor {
		_, _ = fmt.Fprintf(d.Output, f, v...)
	} else {
		color.Fprintf(d.Output, f, v...)
	}
}

func (d *Dumper) write(indent bool, v ...any) {
	if indent && !d.msValue {
		_, _ = d.Output.Write(d.indentBytes)
	}

	if d.NoColor {
		_, _ = fmt.Fprint(d.Output, v...)
	} else {
		color.Fprint(d.Output, v...)
	}
}

func (d *Dumper) indentPrint(v ...any) {
	d.write(true, v...)
}

// printBoolValue 打印布尔值
func (d *Dumper) printBoolValue(t reflect.Type, v reflect.Value) {
	d.printf("%s(%v),\n", t.String(), v.Bool())
}

// printFloatValue 打印浮点数
func (d *Dumper) printFloatValue(t reflect.Type, v reflect.Value) {
	d.printf("%s(%v),\n", t.String(), v.Float())
}

// printIntValue 打印整数
func (d *Dumper) printIntValue(t reflect.Type, v reflect.Value) {
	intStr := strconv.FormatInt(v.Int(), 10)
	intStr = d.ColorTheme.integer(intStr)
	d.printf("%s(%s),%s\n", t.String(), intStr, d.rvStringer(t, v))
}

// printUintValue 打印无符号整数
func (d *Dumper) printUintValue(t reflect.Type, v reflect.Value) {
	intStr := strconv.FormatUint(v.Uint(), 10)
	intStr = d.ColorTheme.integer(intStr)
	d.printf("%s(%s),%s\n", t.String(), intStr, d.rvStringer(t, v))
}

// printStringValue 打印字符串
func (d *Dumper) printStringValue(t reflect.Type, v reflect.Value) {
	strVal := d.ColorTheme.string(v.String())
	lenTip := d.ColorTheme.valTip("#len=" + strconv.Itoa(v.Len()))
	d.printf("%s(\"%s\"), %s\n", t.String(), strVal, lenTip)
}

// printComplexValue 打印复数
func (d *Dumper) printComplexValue(_ reflect.Type, v reflect.Value) {
	d.printf("%#v\n", v.Complex())
}

// printSliceOrArrayValue 打印切片或数组
func (d *Dumper) printSliceOrArrayValue(t reflect.Type, v reflect.Value, isPtr bool) {
	if v.CanAddr() && !d.checkCyclicRef(t, v) {
		return // don't print v again
	}

	eleNum := v.Len()
	lenTip := d.ColorTheme.valTip("#len=" + strconv.Itoa(eleNum) + ",cap=" + strconv.Itoa(v.Cap()))

	d.write(!isPtr, t.String(), " [ ", lenTip, "\n")
	d.msValue = false

	for i := 0; i < eleNum; i++ {
		sv := v.Index(i)
		d.advance(1)
		d.printRValue(sv.Type(), sv)
		d.advance(-1)
	}

	d.indentPrint("],\n")
}

// printStructValue 打印结构体
func (d *Dumper) printStructValue(t reflect.Type, v reflect.Value, isPtr bool) {
	if v.CanAddr() && !d.checkCyclicRef(t, v) {
		return // don't print v again
	}

	// special handle time.Time struct
	if t == timeType {
		timeStr := v.Interface().(time.Time).Format(time.RFC3339)
		d.printf("time.Time(%s),\n", d.ColorTheme.string(timeStr))
		return
	}

	d.write(!isPtr, d.ColorTheme.msType(t.String()), " {\n")
	d.msValue = false

	fldNum := v.NumField()
	for i := 0; i < fldNum; i++ {
		fName := t.Field(i).Name
		if d.SkipPrivate && isUnexported(fName) {
			continue
		}

		fv := v.Field(i)
		if d.SkipNilField && isNilOrInvalid(fv) {
			continue
		}

		d.advance(1)

		// print field name
		d.indentPrint(d.ColorTheme.field(fName), ": ")

		d.msValue = true
		d.printRValue(fv.Type(), fv)
		d.msValue = false

		d.advance(-1)
	}

	d.indentPrint("},\n")
}

// printMapValue 打印 Map
func (d *Dumper) printMapValue(t reflect.Type, v reflect.Value, isPtr bool) {
	lenTip := d.ColorTheme.valTip("#len=" + strconv.Itoa(v.Len()))

	d.write(!isPtr, d.ColorTheme.msType(t.String()), " { ", lenTip, "\n")
	d.msValue = false

	for _, key := range v.MapKeys() {
		mv := v.MapIndex(key)
		if d.SkipNilField && isNilOrInvalid(mv) {
			continue
		}

		d.advance(1)

		// print key name
		if !key.CanInterface() {
			d.printf("%s: ", key.String())
		} else {
			d.printf("%#v: ", key.Interface())
		}

		if mv.CanAddr() && !d.checkCyclicRef(mv.Type(), mv) {
			d.advance(-1)
			continue // don't print mv again
		}

		// print field value
		d.msValue = true
		d.printRValue(mv.Type(), mv)
		d.msValue = false

		d.advance(-1)
	}

	d.indentPrint("},\n")
}

// printInterfaceValue 打印接口
func (d *Dumper) printInterfaceValue(t reflect.Type, v reflect.Value) {
	if v.CanAddr() && !d.checkCyclicRef(t, v) {
		return // don't print v again
	}

	switch e := v.Elem(); {
	case e.Kind() == reflect.Invalid:
		d.indentPrint("nil,\n")
	case e.IsValid():
		d.printRValue(e.Type(), e)
	default:
		d.indentPrint(t.String(), "(nil),\n")
	}
}

// printChanValue 打印通道
func (d *Dumper) printChanValue(t reflect.Type, v reflect.Value) {
	d.printf("(%s)(%#v),\n", t.String(), v.Pointer())
}

// printFuncValue 打印函数
func (d *Dumper) printFuncValue(t reflect.Type, _ reflect.Value) {
	d.printf("(%s) {...},\n", t.String())
}

// printUnsafePointerValue 打印不安全指针
func (d *Dumper) printUnsafePointerValue(_ reflect.Type, v reflect.Value) {
	d.printf("(%#v),\n", v.Pointer())
}

// printInvalidValue 打印无效值
func (d *Dumper) printInvalidValue(_ reflect.Type, v reflect.Value) {
	d.indentPrint(v.Type().String(), "(nil),\n")
}

// printDefaultValue 打印默认值（其他类型）
func (d *Dumper) printDefaultValue(t reflect.Type, v reflect.Value) {
	if v.CanAddr() && !d.checkCyclicRef(t, v) {
		return // don't print v again
	}

	if v.CanInterface() {
		d.printf("%s(%#v),\n", t.String(), v.Interface())
	} else {
		d.printf("%s(%v),\n", t.String(), v.String())
	}
}
