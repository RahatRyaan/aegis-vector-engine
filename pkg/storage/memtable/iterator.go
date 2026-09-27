package memtable

type Iterator struct {
	list *ConcurrentSkiplist
	curr uint32
}

func (s *ConcurrentSkiplist) NewIterator() *Iterator {
	return &Iterator{
		list: s,
		curr: 0,
	}
}

func (it *Iterator) Valid() bool {
	return it.curr != 0 && it.curr != it.list.head
}

func (it *Iterator) SeekToFirst() {
	it.curr = it.list.getNextOffset(it.list.head, 0)
}

func (it *Iterator) Seek(key []byte) {
	it.curr = it.list.findGreaterOrEqual(key, nil)
}

func (it *Iterator) Next() {
	if !it.Valid() {
		return
	}
	it.curr = it.list.getNextOffset(it.curr, 0)
}

func (it *Iterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	return it.list.nodeKey(it.curr)
}

func (it *Iterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	return it.list.nodeValue(it.curr)
}

func (it *Iterator) Seq() uint64 {
	if !it.Valid() {
		return 0
	}
	return it.list.nodeSeq(it.curr)
}

func (it *Iterator) OpType() byte {
	if !it.Valid() {
		return 0
	}
	return it.list.nodeOpType(it.curr)
}
