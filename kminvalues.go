package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"math"
	"sort"
)

const BytesUint64 = 8
const Hash_Max = float64(1<<64 - 1)

var Default_KMinValues_Size = 1 << 10

func HashUint64ToBytes(hash uint64) []byte {
	hashBytes := new(bytes.Buffer)
	binary.Write(hashBytes, binary.BigEndian, hash)
	return hashBytes.Bytes()
}

func HashBytesToUint64(hashBytes []byte) uint64 {
	// TODO: error checking here
	var hash uint64
	hashReader := bytes.NewBuffer(hashBytes)
	binary.Read(hashReader, binary.BigEndian, &hash)
	return hash
}

func Union(others ...*KMinValues) *KMinValues {
	maxsize := smallestK(others...)
	newkmv := NewKMinValues(maxsize)

	for _, other := range others {
		for i := 0; i < other.Len(); i++ {
			newkmv.AddHashBytes(other.GetHashBytes(i))
		}
	}
	return newkmv
}

func cardinality(maxSize int, kMin uint64) float64 {
	return float64(maxSize-1.0) * Hash_Max / float64(kMin)
}

func smallestK(others ...*KMinValues) int {
	minsize := others[0].MaxSize
	for _, other := range others[1:] {
		if minsize > other.MaxSize {
			minsize = other.MaxSize
		}
	}
	return minsize
}

type KMinValues struct {
	Raw     []byte `json:'data'`
	MaxSize int    `json:'k'`
}

func NewKMinValues(capacity int) *KMinValues {
	return &KMinValues{
		Raw:     make([]byte, 0, capacity*BytesUint64),
		MaxSize: capacity,
	}
}

func KMinValuesFromBytes(raw []byte) *KMinValues {
	if len(raw) == 0 {
		return NewKMinValues(Default_KMinValues_Size)
	}
	buf := bytes.NewBuffer(raw)

	var maxSizeTmp uint64
	var maxSize int
	err := binary.Read(buf, binary.BigEndian, &maxSizeTmp)
	if err != nil {
		log.Println("error reading size")
		return NewKMinValues(Default_KMinValues_Size)
	}
	maxSize = int(maxSizeTmp)

	kmv := KMinValues{
		Raw:     raw[BytesUint64:],
		MaxSize: maxSize,
	}
	return &kmv
}

func (kmv *KMinValues) GetHash(i int) uint64 {
	hashBytes := kmv.Raw[i*BytesUint64 : (i+1)*BytesUint64]
	return HashBytesToUint64(hashBytes)
}

func (kmv *KMinValues) GetHashBytes(i int) []byte {
	return kmv.Raw[i*BytesUint64 : (i+1)*BytesUint64]
}

func (kmv *KMinValues) Bytes() []byte {
	sizeBytes := make([]byte, BytesUint64, BytesUint64+len(kmv.Raw))
	binary.BigEndian.PutUint64(sizeBytes, uint64(kmv.MaxSize))
	result := append(sizeBytes, kmv.Raw...)
	return result
}

func (kmv *KMinValues) Len() int { return len(kmv.Raw) / BytesUint64 }

func (kmv *KMinValues) Less(i, j int) bool {
	// Reversed logic for reverse order
	// Also, we use BigEndian ordering to make this easy
	return bytes.Compare(kmv.GetHashBytes(i), kmv.GetHashBytes(j)) > 0
}

func (kmv *KMinValues) Swap(i, j int) {
	ib, jb := i*BytesUint64, j*BytesUint64
	for n := 0; n < BytesUint64; n++ {
		kmv.Raw[ib+n], kmv.Raw[jb+n] = kmv.Raw[jb+n], kmv.Raw[ib+n]
	}
}

func (kmv *KMinValues) SetHash(i int, hash []byte) {
	ib := i * BytesUint64
	for n := 0; n < BytesUint64; n++ {
		kmv.Raw[ib+n] = hash[n]
	}
}

func (kmv *KMinValues) FindHash(hash uint64) int {
	hashBytes := HashUint64ToBytes(hash)
	return kmv.FindHashBytes(hashBytes)
}

func (kmv *KMinValues) FindHashBytes(hash []byte) int {
	found := sort.Search(kmv.Len(), func(i int) bool { return bytes.Compare(kmv.GetHashBytes(i), hash) <= 0 })
	if found < kmv.Len() && bytes.Equal(kmv.GetHashBytes(found), hash) {
		return found
	}
	return -1
}

func (kmv *KMinValues) AddHash(hash uint64) bool {
	hashBytes := HashUint64ToBytes(hash)
	return kmv.AddHashBytes(hashBytes)
}

// Adds a hash to the KMV and maintains the sorting of the values.
// Furthermore, we make sure that items we are inserting are unique by
// searching for them prior to insertion.  We wait to do this seach last
// because it is computationally expensive so we attempt to throw away the hash
// in every way possible before performing it.
func (kmv *KMinValues) AddHashBytes(hash []byte) bool {
	n := kmv.Len()
	if n >= kmv.MaxSize {
		if bytes.Compare(kmv.GetHashBytes(0), hash) < 0 {
			return false
		}
		if kmv.FindHashBytes(hash) == -1 {
			kmv.SetHash(0, hash)
		} else {
			return false
		}
	} else {
		if kmv.FindHashBytes(hash) == -1 {
			if cap(kmv.Raw) == len(kmv.Raw)+1 {
				kmv.increaseCapacity(len(kmv.Raw) * 2)
			}
			kmv.Raw = append(kmv.Raw, hash...)
		} else {
			return false
		}
	}
	sort.Sort(kmv)
	return true
}

// Adds extra capacity to the underlying []uint64 array that stores the hashes
func (kmv *KMinValues) increaseCapacity(newcap int) bool {
	N := cap(kmv.Raw)
	if newcap < N {
		return false
	}
	if newcap/BytesUint64 > kmv.MaxSize {
		if N == kmv.MaxSize*BytesUint64 {
			return false
		}
		newcap = kmv.MaxSize * BytesUint64
	}
	newarray := make([]byte, len(kmv.Raw), newcap)
	for i := 0; i < len(kmv.Raw); i++ {
		newarray[i] = kmv.Raw[i]
	}
	kmv.Raw = newarray
	return true
}

func (kmv *KMinValues) Cardinality() float64 {
	if kmv.Len() < kmv.MaxSize {
		return float64(kmv.Len())
	}
	return cardinality(kmv.MaxSize, kmv.GetHash(0))
}

func (kmv *KMinValues) CardinalityIntersection(others ...*KMinValues) float64 {
	X, n := DirectSum(append(others, kmv)...)
	return float64(n) / float64(X.MaxSize) * X.Cardinality()

}

func (kmv *KMinValues) CardinalityUnion(others ...*KMinValues) float64 {
	X, _ := DirectSum(append(others, kmv)...)
	return X.Cardinality()

}

func (kmv *KMinValues) Jaccard(others ...*KMinValues) float64 {
	X, n := DirectSum(append(others, kmv)...)
	return float64(n) / float64(X.MaxSize)
}

// Returns a new KMinValues object is the union between the current and the
// given objects
func (kmv *KMinValues) Union(others ...*KMinValues) *KMinValues {
	return Union(append(others, kmv)...)
}

func (kmv *KMinValues) RelativeError() float64 {
	return math.Sqrt(2.0 / (math.Pi * float64(kmv.MaxSize-2)))
}

func DirectSum(others ...*KMinValues) (*KMinValues, int) {
	n := 0
	X := Union(others...)
	// TODO: can we optimize this loop somehow?
	var found bool
	for i := 0; i < X.Len(); i++ {
		xHash := X.GetHashBytes(i)
		found = true
		for _, other := range others {
			if other.FindHashBytes(xHash) < 0 {
				found = false
				break
			}
		}
		if found {
			n += 1
		}
	}
	return X, n
}
