package vs

import (
	"github.com/chrislusf/vasto/util"
)

// KeyObject locates a entry, usually by a []byte as a key.
// Additionally, there could be multiple store partitions. A partition key or partition hash can be used to locate
// the store partitions. Prefix queries can be fairly fast if the entries shares the same partition.
type KeyObject struct {
	key           []byte
	partitionHash uint64
}

func Key(key []byte) *KeyObject {
	return &KeyObject{
		key:           key,
		partitionHash: util.Hash(key),
	}
}

func (k *KeyObject) SetPartitionKey(partitionKey []byte) *KeyObject {
	k.partitionHash = util.Hash(partitionKey)
	return k
}

func (k *KeyObject) SetPartitionHash(partitionHash uint64) *KeyObject {
	k.partitionHash = partitionHash
	return k
}

func (k *KeyObject) GetKey() []byte {
	return k.key
}

func (k *KeyObject) GetPartitionHash() uint64 {
	return k.partitionHash
}