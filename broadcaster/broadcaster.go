package broadcaster

type Broadcaster[Data any] struct {
	input chan Data
	reg   chan chan<- Data
	unreg chan chan<- Data

	outputs map[chan<- Data]bool
}

func NewBroadcaster[Data any](buflen int) *Broadcaster[Data] {
	b := &Broadcaster[Data]{
		input:   make(chan Data, buflen),
		reg:     make(chan chan<- Data),
		unreg:   make(chan chan<- Data),
		outputs: make(map[chan<- Data]bool),
	}

	go b.run()

	return b
}

func (b *Broadcaster[Data]) Register(ch chan<- Data) chan<- Data {
	b.reg <- ch

	return ch
}

func (b *Broadcaster[Data]) Unregister(ch chan<- Data) {
	b.unreg <- ch
}

func (b *Broadcaster[Data]) Close() error {
	close(b.reg)
	close(b.unreg)
	return nil
}

// Submit an item to be broadcast to all listeners.
func (b *Broadcaster[Data]) Submit(m Data) {
	b.input <- m
}

// TrySubmit attempts to submit an item to be broadcast, returning
// true iff it the item was broadcast, else false.
func (b *Broadcaster[Data]) TrySubmit(m Data) bool {
	select {
	case b.input <- m:
		return true
	default:
		return false
	}
}

func (b *Broadcaster[Data]) broadcast(m Data) {
	for ch := range b.outputs {
		ch <- m
	}
}

func (b *Broadcaster[Data]) run() {
	for {
		select {
		case m := <-b.input:
			b.broadcast(m)
		case ch, ok := <-b.reg:
			if !ok {
				return
			}

			b.outputs[ch] = true
		case ch := <-b.unreg:
			delete(b.outputs, ch)
		}
	}
}
