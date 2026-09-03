package highlight

import (
	"context"
	"io"

	"github.com/oligo/gvcode/textstyle/syntax"
)

type Request struct {
	Tag        string
	Generation uint64
	Language   Language
	Reader     io.Reader
}
type Result struct {
	Tag        string
	Generation uint64
	Tokens     []syntax.Token
	Err        error
}
type Service struct {
	ctx      context.Context
	cancel   context.CancelFunc
	requests chan Request
	results  chan Result
	wake     func()
}

func NewService(wake func()) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{ctx: ctx, cancel: cancel, requests: make(chan Request, 1), results: make(chan Result, 2), wake: wake}
	go s.loop()
	return s
}
func (s *Service) Submit(r Request) {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	select {
	case s.requests <- r:
		return
	default:
	}
	select {
	case <-s.requests:
	default:
	}
	select {
	case s.requests <- r:
	case <-s.ctx.Done():
	}
}
func (s *Service) Results() <-chan Result { return s.results }
func (s *Service) Close()                 { s.cancel() }
func (s *Service) loop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case req := <-s.requests:
			data, err := io.ReadAll(req.Reader)
			var toks []syntax.Token
			if err == nil {
				toks, err = Tokens(s.ctx, req.Language, string(data))
			}
			res := Result{Tag: req.Tag, Generation: req.Generation, Tokens: toks, Err: err}
			select {
			case s.results <- res:
			default:
				select {
				case <-s.results:
				default:
				}
				select {
				case s.results <- res:
				default:
				}
			}
			if s.wake != nil {
				s.wake()
			}
		}
	}
}
