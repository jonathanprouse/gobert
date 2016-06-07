package bert

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"math"
	"reflect"
	"strconv"
	"math/big"
)

var (
	ErrBadMagic    error = errors.New("bad magic")
	ErrUnknownType error = errors.New("unknown type")
	ErrMissingAtom error = errors.New("missing Atom")

	// The atom distribution cache
	cache = DistributionHeader{}
)

func read1(r io.Reader) (int, error) {
	bits, err := ioutil.ReadAll(io.LimitReader(r, 1))
	if err != nil {
		return 0, err
	}

	ui8 := uint8(bits[0])
	return int(ui8), nil
}

func read2(r io.Reader) (int, error) {
	bits, err := ioutil.ReadAll(io.LimitReader(r, 2))
	if err != nil {
		return 0, err
	}

	ui16 := binary.BigEndian.Uint16(bits)
	return int(ui16), nil
}

func read4(r io.Reader) (int, error) {
	bits, err := ioutil.ReadAll(io.LimitReader(r, 4))
	if err != nil {
		return 0, err
	}

	ui32 := binary.BigEndian.Uint32(bits)
	return int(ui32), nil
}

func readCompressed(r io.Reader) (Term, error) {
	_, err := read4(r)
	if err != nil {
		return nil, err
	}

	// Attempt to decode the bytes
	reader, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	// Start reading from the new reader
	return readTag(reader)
}

func readDistributionHeader(r io.Reader) (Term, error) {

	// Attempt to parse the header into the cache
	if err := cache.Update(r); err != nil {
		return nil, err
	}

	// Cache has now been updated so parse the next flag
	return readTag(r)
}

func readSmallInt(r io.Reader) (int, error) {
	return read1(r)
}

func readInt(r io.Reader) (int, error) {

	// An integer is a Signed 32bit value
	// Depending on whether we are on a 32 or 64 bit system the default
	// int size will change appropriately. Therefore the sign of
	// a number will be lost when compiling on a 64 bit system but
	// will work on a 32 bit. The way around this is to cast to a
	// int32 and then cast back to an int which will keep the sign of the number
	val, err := read4(r)
	if err != nil {
		return val, err
	}

	return int(int32(val)), nil
}

func readSmallBignum(r io.Reader) (big.Int, error) {
	numLen, err := read1(r)
	if err != nil {
		return *big.NewInt(0), err
	}
	return readBigNum(r, numLen)
}

func readLargeBignum(r io.Reader) (big.Int, error) {
	numLen, err := read4(r)
	if err != nil {
		return *big.NewInt(0), err
	}
	return readBigNum(r, numLen)
}

func readBigNum(r io.Reader, numLen int) (big.Int, error) {
	sign, err := read1(r)
	if err != nil {
		return *big.NewInt(0), err
	}

	bits, err := ioutil.ReadAll(io.LimitReader(r, int64(numLen)))
	if err != nil {
		return *big.NewInt(0), err
	}

	// The bytes are stored with the LSB byte stored first
	// Reverse the array to get BigEndian
	var bigEndBits []byte
	for i := len(bits)-1; i >= 0; i-- {
		bigEndBits = append(bigEndBits, bits[i])
	}

	// Parse the big int
	bigNum := &big.Int{}
	bigNum.SetBytes(bigEndBits)
	if sign == 1 {

		// Then the number is negative
		bigNum = bigNum.Neg(bigNum)
	}
	return *bigNum, nil
}

func readFloat(r io.Reader) (float32, error) {
	bits, err := ioutil.ReadAll(io.LimitReader(r, 31))
	if err != nil {
		return 0, err
	}

	// ParseFloat doesn't like trailing 0s
	var i int
	for i = 0; i < len(bits); i++ {
		if bits[i] == 0 {
			break
		}
	}

	f, err := strconv.ParseFloat(string(bits[0:i]), 32)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}

func readNewFloat(r io.Reader) (float64, error) {
	bits, err := ioutil.ReadAll(io.LimitReader(r, 8))
	if err != nil {
		return 0, err
	}

	ui64 := binary.BigEndian.Uint64(bits)
	return math.Float64frombits(ui64), nil
}

func readAtomRef(r io.Reader) (Atom, error) {
	atomCacheRefIndex, err := read1(r)
	if err != nil {
		return Atom(""), err
	}
	atom, err := cache.GetAtom(uint8(atomCacheRefIndex))
	if err != nil {
		return Atom(""), err
	}
	return *atom, nil
}

func readAtom(r io.Reader) (Atom, error) {
	str, err := readString(r)
	return Atom(str), err
}

func readSmallAtom(r io.Reader) (Atom, error) {
	str, err := readSmallString(r)
	return Atom(str), err
}

func readSmallTuple(r io.Reader) (Term, error) {
	size, err := read1(r)
	if err != nil {
		return nil, err
	}

	tuple := make([]Term, size)

	for i := 0; i < size; i++ {
		term, err := readTag(r)
		if err != nil {
			return nil, err
		}
		switch a := term.(type) {
		case Atom:
			if a == BertAtom {
				return readComplex(r)
			}
		}
		tuple[i] = term
	}

	return tuple, nil
}

func readLargeTuple(r io.Reader) (Term, error) {
	size, err := read4(r)
	if err != nil {
		return nil, err
	}

	tuple := make([]Term, size)

	for i := uint32(0); i < uint32(size); i++ {
		term, err := readTag(r)
		if err != nil {
			return nil, err
		}
		switch a := term.(type) {
		case Atom:
			if a == BertAtom {
				return readComplex(r)
			}
		}
		tuple[i] = term
	}

	return tuple, nil
}

func readNil(r io.Reader) ([]Term, error) {
	_, err := ioutil.ReadAll(io.LimitReader(r, 1))
	if err != nil {
		return nil, err
	}
	list := make([]Term, 0)
	return list, nil
}

func readString(r io.Reader) (string, error) {
	size, err := read2(r)
	if err != nil {
		return "", err
	}

	str, err := ioutil.ReadAll(io.LimitReader(r, int64(size)))
	if err != nil {
		return "", err
	}

	return string(str), nil
}

func readSmallString(r io.Reader) (string, error) {
	size, err := read1(r)
	if err != nil {
		return "", err
	}

	str, err := ioutil.ReadAll(io.LimitReader(r, int64(size)))
	if err != nil {
		return "", err
	}

	return string(str), nil
}

func readList(r io.Reader) ([]Term, error) {
	size, err := read4(r)
	if err != nil {
		return nil, err
	}

	list := make([]Term, size)

	for i := 0; i < size; i++ {
		term, err := readTag(r)
		if err != nil {
			return nil, err
		}
		list[i] = term
	}

	read1(r)

	return list, nil
}

func readBin(r io.Reader) ([]uint8, error) {
	size, err := read4(r)
	if err != nil {
		return []uint8{}, err
	}

	bytes, err := ioutil.ReadAll(io.LimitReader(r, int64(size)))
	if err != nil {
		return []uint8{}, err
	}

	return bytes, nil
}

func readMap(r io.Reader) (map[Term]Term, error) {
	pairs, err := read4(r)
	if err != nil {
		return nil, err
	}

	m := make(map[Term]Term)

	for i := 0; i < pairs; i++ {
		key, err := readTag(r)
		if err != nil {
			return nil, err
		}
		value, err := readTag(r)
		if err != nil {
			return nil, err
		}
		m[key] = value
	}

	return m, nil
}

func readComplex(r io.Reader) (Term, error) {
	term, err := readTag(r)

	if err != nil {
		return term, err
	}

	switch kind := term.(type) {
	case Atom:
		switch kind {
		case NilAtom:
			return nil, nil
		case TrueAtom:
			return true, nil
		case FalseAtom:
			return false, nil
		}
	}

	return term, nil
}

func readReference(r io.Reader) (Reference, error) {
	reference := Reference{}

	term, err := readTag(r)
	if err != nil {
		return reference, err
	}

	switch a := term.(type) {
	case Atom:
		reference.Node = a
	default:
		return reference, ErrMissingAtom
	}

	id, err := read4(r)
	if err != nil {
		return reference, err
	}
	reference.ID = uint32(id)

	creation, err := read1(r)
	if err != nil {
		return reference, err
	}
	reference.Creation = uint8(creation)

	return reference, nil
}

func readNewReference(r io.Reader) (NewReference, error) {
	reference := NewReference{}

	len, err := read2(r)
	if err != nil {
		return reference, err
	}

	term, err := readTag(r)
	if err != nil {
		return reference, err
	}

	switch a := term.(type) {
	case Atom:
		reference.Node = a
	default:
		return reference, ErrMissingAtom
	}

	creation, err := read1(r)
	if err != nil {
		return reference, err
	}
	reference.Creation = uint8(creation)

	// Extract the IDS
	ids := make([]uint32, len)
	for i := 0; i < len; i++ {
		id, err := read4(r)
		if err != nil {
			return reference, err
		}
		ids[i] = uint32(id)
	}
	reference.ID = ids

	return reference, nil
}

func readPort(r io.Reader) (Port, error) {
	port := Port{}

	reference, err := readReference(r)
	if err != nil {
		return port, err
	}

	port.Reference = reference
	return port, nil
}

func readPid(r io.Reader) (Pid, error) {
	pid := Pid{}

	term, err := readTag(r)
	if err != nil {
		return pid, err
	}

	switch a := term.(type) {
	case Atom:
		pid.Node = a
	default:
		return pid, ErrMissingAtom
	}

	id, err := read4(r)
	if err != nil {
		return pid, err
	}
	pid.ID = uint32(id)

	serial, err := read4(r)
	if err != nil {
		return pid, err
	}
	pid.Serial = uint32(serial)

	creation, err := read1(r)
	if err != nil {
		return pid, err
	}
	pid.Creation = uint8(creation)

	return pid, nil
}

func readFunc(r io.Reader) (Func, error) {
	function := Func{}

	numfree, err := read4(r)
	if err != nil {
		return function, err
	}

	pid, err := readPid(r)
	if err != nil {
		return function, err
	}
	function.Pid = pid

	term, err := readTag(r)
	if err != nil {
		return function, err
	}

	switch module := term.(type) {
	case Atom:
		function.Module = module
	default:
		return function, ErrMissingAtom
	}

	term, err = readTag(r)
	if err != nil {
		return function, err
	}

	switch v := reflect.ValueOf(term); v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		function.Index = uint32(v.Int())
	default:
		return function, ErrUnknownType
	}

	term, err = readTag(r)
	if err != nil {
		return function, err
	}

	switch v := reflect.ValueOf(term); v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		function.Uniq = uint32(v.Int())
	default:
		return function, ErrUnknownType
	}

	// Extract the free vars
	freeVars := make([]Term, numfree)
	for i := 0; i < numfree; i++ {
		term, err := readTag(r)
		if err != nil {
			return function, err
		}
		freeVars[i] = term
	}
	function.FreeVars = freeVars

	return function, nil
}

func readNewFunc(r io.Reader) (NewFunc, error) {
	function := NewFunc{}

	// Get size of the func including the 4 bytes itself
	size, err := read4(r)
	if err != nil {
		return function, err
	}

	// Only allow the next size-4 bytes to be read
	lr := io.LimitReader(r, int64(size-4))

	arity, err := read1(lr)
	if err != nil {
		return function, err
	}
	function.Arity = uint8(arity)

	uniq, err := ioutil.ReadAll(io.LimitReader(r, 16))
	if err != nil {
		return function, err
	}
	function.Uniq = uniq

	term, err := readTag(lr)
	if err != nil {
		return function, err
	}

	switch v := reflect.ValueOf(term); v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		function.Index = uint32(v.Int())
	}

	numfree, err := read4(lr)
	if err != nil {
		return function, err
	}

	term, err = readTag(lr)
	if err != nil {
		return function, err
	}

	switch module := term.(type) {
	case Atom:
		function.Module = module
	default:
		return function, ErrMissingAtom
	}

	term, err = readTag(lr)
	if err != nil {
		return function, err
	}

	switch v := reflect.ValueOf(term); v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		function.OldIndex = uint32(v.Int())
	default:
		return function, ErrUnknownType
	}

	term, err = readTag(lr)
	if err != nil {
		return function, err
	}

	switch v := reflect.ValueOf(term); v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		function.OldUnique = uint32(v.Int())
	default:
		return function, ErrUnknownType
	}

	pid, err := readPid(lr)
	if err != nil {
		return function, err
	}
	function.Pid = pid

	// Extract the free vars
	freeVars := make([]Term, numfree)
	for i := 0; i < numfree; i++ {
		term, err := readTag(lr)
		if err != nil {
			return function, err
		}
		freeVars[i] = term
	}
	function.FreeVars = freeVars

	return function, nil
}

func readExport(r io.Reader) (Export, error) {
	export := Export{}

	term, err := readTag(r)
	if err != nil {
		return export, err
	}

	switch module := term.(type) {
	case Atom:
		export.Module = module
	default:
		return export, ErrMissingAtom
	}

	term, err = readTag(r)
	if err != nil {
		return export, err
	}

	switch function := term.(type) {
	case Atom:
		export.Function = function
	default:
		return export, ErrMissingAtom
	}

	arity, err := readSmallInt(r)
	if err != nil {
		return export, err
	}
	export.Arity = uint8(arity)

	return export, nil
}

func readTag(r io.Reader) (Term, error) {
	tag, err := read1(r)
	if err != nil {
		return nil, err
	}

	switch tag {
	case CompressedTag:
		return readCompressed(r)
	case DistributionHeaderTag:
		return readDistributionHeader(r)
	case SmallIntTag:
		return readSmallInt(r)
	case IntTag:
		return readInt(r)
	case SmallBignumTag:
		return readSmallBignum(r)
	case LargeBignumTag:
		return readLargeBignum(r)
	case FloatTag:
		return readFloat(r)
	case NewFloatTag:
		return readNewFloat(r)
	case AtomCacheRefTag:
		return readAtomRef(r)
	case AtomTag, AtomUtf8Tag:
		return readAtom(r)
	case SmallAtomTag, SmallAtomUtf8Tag:
		return readSmallAtom(r)
	case SmallTupleTag:
		return readSmallTuple(r)
	case LargeTupleTag:
		return readLargeTuple(r)
	case NilTag:
		return readNil(r)
	case StringTag:
		return readString(r)
	case ListTag:
		return readList(r)
	case BinTag:
		return readBin(r)
	case MapTag:
		return readMap(r)
	case ReferenceTag:
		return readReference(r)
	case NewReferenceTag:
		return readNewReference(r)
	case PortTag:
		return readPort(r)
	case PidTag:
		return readPid(r)
	case FunTag:
		return readFunc(r)
	case NewFunTag:
		return readNewFunc(r)
	case ExportTag:
		return readExport(r)
	}

	return nil, ErrUnknownType
}

// DecodeFrom decodes a Term from r and returns it or an error.
func DecodeFrom(r io.Reader) (Term, error) {
	version, err := read1(r)
	if err != nil {
		return nil, err
	}

	// check protocol version
	if version != VersionTag {
		return nil, ErrBadMagic
	}

	return readTag(r)
}

// Decode decodes a Term from data and returns it or an error.
func Decode(data []byte) (Term, error) { return DecodeFrom(bytes.NewBuffer(data)) }

// UnmarshalFrom decodes a value from r, stores it in val, and returns any
// error encountered.
func UnmarshalFrom(r io.Reader, val interface{}) (err error) {
	result, _ := DecodeFrom(r)

	value := reflect.ValueOf(val).Elem()

	switch v := value; v.Kind() {
	case reflect.Struct:
		slice := reflect.ValueOf(result)
		for i := 0; i < slice.Len(); i++ {
			e := slice.Index(i).Elem()
			v.Field(i).Set(e)
		}
	}

	return nil
}

// Unmarshal decodes a value from data, stores it in val, and returns any error
// encountered.
func Unmarshal(data []byte, val interface{}) (err error) {
	return UnmarshalFrom(bytes.NewBuffer(data), val)
}

// UnmarshalRequest decodes a BURP from r and returns it as a Request.
func UnmarshalRequest(r io.Reader) (Request, error) {
	var req Request

	size, err := read4(r)
	if err != nil {
		return req, err
	}

	err = UnmarshalFrom(io.LimitReader(r, int64(size)), &req)

	return req, err
}
