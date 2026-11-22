// Package dex implements a dependency-free reader for the Dalvik Executable
// format (035 through 041) and the APK/ZIP containers that carry it.
//
// The parser is intentionally eager: every DEX section is decoded into plain
// Go structures so the SQL materializer can load it without re-reading bytes.
package dex

// Header mirrors the 112-byte DEX header.
type Header struct {
	Version      string
	Checksum     uint32
	Signature    [20]byte
	FileSize     uint32
	HeaderSize   uint32
	EndianTag    uint32
	LinkSize     uint32
	LinkOff      uint32
	MapOff       uint32
	StringCount  uint32
	StringIDsOff uint32
	TypeCount    uint32
	TypeIDsOff   uint32
	ProtoCount   uint32
	ProtoIDsOff  uint32
	FieldCount   uint32
	FieldIDsOff  uint32
	MethodCount  uint32
	MethodIDsOff uint32
	ClassCount   uint32
	ClassDefsOff uint32
	DataSize     uint32
	DataOff      uint32
}

// MapItem is one entry of the DEX map_list.
type MapItem struct {
	TypeCode uint16
	Size     uint32
	Offset   uint32
}

// Proto is a method prototype (proto_ids entry) with its parameter list.
type Proto struct {
	Index         uint32
	ShortyIdx     uint32
	Shorty        string
	ReturnTypeIdx uint32
	ReturnDesc    string
	ParametersOff uint32
	ParamTypeIdx  []uint16
	ParamDescs    []string
}

// FieldID is a field_ids entry.
type FieldID struct {
	Index     uint32
	ClassIdx  uint16
	TypeIdx   uint16
	NameIdx   uint32
	ClassDesc string
	TypeDesc  string
	Name      string
}

// Descriptor returns the full "Lpkg/Cls;->field:Type" form.
func (f FieldID) Descriptor() string {
	return f.ClassDesc + "->" + f.Name + ":" + f.TypeDesc
}

// MethodID is a method_ids entry.
type MethodID struct {
	Index      uint32
	ClassIdx   uint16
	ProtoIdx   uint16
	NameIdx    uint32
	ClassDesc  string
	Name       string
	Descriptor string
	ReturnDesc string
	ParamDescs []string
}

// EncodedField is a field definition found in a class_data_item.
type EncodedField struct {
	FieldIdx    uint32
	AccessFlags uint32
	Kind        string // "static" or "instance"
}

// EncodedMethod is a method definition found in a class_data_item.
type EncodedMethod struct {
	MethodIdx   uint32
	AccessFlags uint32
	CodeOff     uint32
	Kind        string // "direct" or "virtual"
}

// ClassDef is a class_def_item plus its decoded class_data and interfaces.
type ClassDef struct {
	Index           uint32
	ClassIdx        uint32
	Descriptor      string
	AccessFlags     uint32
	SuperclassIdx   uint32
	HasSuperclass   bool
	SuperDescriptor string
	InterfacesOff   uint32
	Interfaces      []uint16
	InterfaceDescs  []string
	SourceFileIdx   uint32
	HasSourceFile   bool
	SourceFile      string
	AnnotationsOff  uint32
	ClassDataOff    uint32
	StaticValuesOff uint32
	ClassData       *ClassData
	StaticValues    []EncodedValue
}

// ClassData is a decoded class_data_item.
type ClassData struct {
	Off            uint32
	EndOff         uint32
	StaticFields   []EncodedField
	InstanceFields []EncodedField
	DirectMethods  []EncodedMethod
	VirtualMethods []EncodedMethod
}

// TryItem is one protected range in a code_item.
type TryItem struct {
	Index      uint32
	StartAddr  uint32
	InsnCount  uint32
	HandlerOff uint16
	Handlers   []CatchHandler
}

// CatchHandler is an expanded typed catch or catch-all entry.
type CatchHandler struct {
	HandlerIdx uint32
	HandlerOff uint16
	Ordinal    uint32
	TypeIdx    uint32
	HasType    bool
	TypeDesc   string
	Addr       uint32
	IsCatchAll bool
}

// CodeItem is a decoded code_item.
type CodeItem struct {
	MethodIdx     uint32
	CodeOff       uint32
	RegistersSize uint16
	InsSize       uint16
	OutsSize      uint16
	TriesSize     uint16
	DebugInfoOff  uint32
	InsnsSize     uint32
	InsnsOff      uint32
	Insns         []uint16
	Tries         []TryItem
	HandlersOff   uint32
	OpcodeCount   uint32
	OpcodeHex     string
	Refs          []InsnRef
	Numbers       []Number
}

// Number is an immediate constant loaded by a const instruction.
type Number struct {
	Opcode     uint8
	NumberType string
	LongValue  int64
	DoubleVal  float64
	HasDouble  bool
}

// InsnRef is a bytecode reference to a string, type, field, method, proto or
// method handle index.
type InsnRef struct {
	Kind      string // string | type | field | method | proto | method_handle
	Index     uint32
	UsingType string // "get" / "put" for field refs, otherwise ""
	Addr      uint32 // instruction address in 16-bit code units
}

// Annotation is one annotation_item.
type Annotation struct {
	Off          uint32
	Visibility   uint32
	TypeIdx      uint32
	TypeDesc     string
	ElementCount uint32
	Elements     []AnnotationElement
}

// AnnotationElement is one encoded value node inside an annotation.
type AnnotationElement struct {
	Depth       uint32
	Ordinal     uint32
	NameIdx     uint32
	HasName     bool
	Name        string
	ValueOff    uint32
	ValueEndOff uint32
	Value       EncodedValue
}

// AnnotationDirectory describes an annotations_directory_item.
type AnnotationDirectory struct {
	ClassIdx       uint32
	AnnotationsOff uint32
	ClassSetOff    uint32
	HasClassSet    bool
	Fields         []AnnotationDirectoryEntry
	Methods        []AnnotationDirectoryEntry
	Parameters     []AnnotationDirectoryEntry
}

// AnnotationDirectoryEntry is one field/method/parameter annotation reference.
type AnnotationDirectoryEntry struct {
	Kind             string // field | method | parameter
	EntryIdx         uint32
	AnnotationSetOff uint32
	RefListOff       uint32
}

// AnnotationSet is a decoded annotation_set_item.
type AnnotationSet struct {
	Off      uint32
	Size     uint32
	Elements []uint32 // annotation_item offsets
}

// AnnotationRefList is a decoded annotation_set_ref_list (parameter annotations).
type AnnotationRefList struct {
	Off    uint32
	Sets   []uint32
	HasSet []bool
}

// EncodedValue is one decoded encoded_value node.
type EncodedValue struct {
	Kind     string
	ValueIdx uint32
	HasIdx   bool
	I64      int64
	F64      float64
	HasFloat bool
	Off      uint32
	EndOff   uint32
}

// CallSite is a call_site_id_item (DEX 038+).
type CallSite struct {
	Index           uint32
	Off             uint32
	ValueCount      uint32
	MethodHandleIdx uint32
	HasHandle       bool
	NameIdx         uint32
	HasName         bool
	ProtoIdx        uint32
	HasProto        bool
	Args            []EncodedValue
}

// MethodHandle is a method_handle_item (DEX 038+).
type MethodHandle struct {
	Index     uint32
	Type      uint32
	MemberIdx uint32
}

// DebugInfo summarises a debug_info_item.
type DebugInfo struct {
	Off            uint32
	LineStart      uint32
	ParametersSize uint32
	ParamNameIdx   []uint32 // 0xffffffff means "no name"
}

// Position is a source-position record from the debug state machine.
type Position struct {
	Address       uint32
	Line          int32
	SourceFileIdx uint32
	HasSourceFile bool
	PrologueEnd   bool
	EpilogueBegin bool
}

// Local is a local-variable event from the debug state machine.
type Local struct {
	Address  uint32
	Event    string
	Register uint32
	NameIdx  uint32
	HasName  bool
	TypeIdx  uint32
	HasType  bool
	SigIdx   uint32
	HasSig   bool
}

// File is one logical DEX inside an input container.
type File struct {
	raw                []byte
	FileID             uint32
	SourcePath         string
	EntryName          string
	ByteLen            int64
	Version            string
	Header             Header
	Strings            []string
	StringOffsets      []uint32
	TypeDescs          []string
	TypeStringIdx      []uint32
	Protos             []Proto
	Fields             []FieldID
	Methods            []MethodID
	Classes            []ClassDef
	MapItems           []MapItem
	MethodHandles      []MethodHandle
	CallSites          []CallSite
	CodeItems          map[uint32]*CodeItem
	DebugInfos         map[uint32]*DebugInfo
	DebugParams        map[uint32][]uint32
	Positions          map[uint32][]Position
	Locals             map[uint32][]Local
	Annotations        []*Annotation
	AnnotationDirs     []*AnnotationDirectory
	AnnotationSets     []*AnnotationSet
	AnnotationRefLists []*AnnotationRefList
	EncodedArrays      []*EncodedArray
	Warnings           []string
}

// EncodedArray is a decoded encoded_array_item with its owner link.
type EncodedArray struct {
	Index     uint32
	Off       uint32
	EndOff    uint32
	OwnerKind string
	OwnerIdx  uint32
	Values    []EncodedValue
	All       []AnnotationElement // flattened, depth/ordinal populated
}
