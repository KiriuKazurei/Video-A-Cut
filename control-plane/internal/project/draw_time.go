package project

import (
	"fmt"
	"math/big"
)

type DrawFrameContext struct {
	Active      bool  `json:"active"`
	ParentFrame int64 `json:"parent_frame"`
	Frame       int64 `json:"frame"`
	FPS         Rate  `json:"fps"`
}

func drawingFrameOffset(delta int64, speed *Rate, source, parent Rate) (int64, error) {
	s := Rate{Num: 1, Den: 1}
	if speed != nil {
		s = *speed
	}
	if delta < 0 || s.Num < 1 || s.Den < 1 || source.Num < 1 || source.Den < 1 || parent.Num < 1 || parent.Den < 1 {
		return 0, fmt.Errorf("invalid drawing clock")
	}
	n := big.NewInt(delta)
	for _, factor := range []int64{s.Num, source.Num, parent.Den} {
		n.Mul(n, big.NewInt(factor))
	}
	d := big.NewInt(s.Den)
	d.Mul(d, big.NewInt(source.Den))
	d.Mul(d, big.NewInt(parent.Num))
	n.Quo(n, d)
	if !n.IsInt64() {
		return 0, fmt.Errorf("drawing clock overflow")
	}
	return n.Int64(), nil
}

// InstanceFrame is the authoritative counterpart of the browser-safe
// draw-time module. Group children use parent-local integer frames.
func (d DrawDocument) InstanceFrame(id string, global int64) (DrawFrameContext, error) {
	cache := map[string]DrawFrameContext{}
	visiting := map[string]bool{}
	var resolve func(string) (DrawFrameContext, error)
	resolve = func(id string) (DrawFrameContext, error) {
		if cached, ok := cache[id]; ok {
			return cached, nil
		}
		if visiting[id] {
			return DrawFrameContext{}, fmt.Errorf("drawing time graph contains a cycle")
		}
		visiting[id] = true
		var node DrawNode
		for _, candidate := range d.Nodes {
			if candidate.ID == id {
				node = candidate
				break
			}
		}
		if node.ID == "" {
			return DrawFrameContext{}, fmt.Errorf("missing drawing time node")
		}
		parentFrame, active := global, true
		if node.ParentID != "" {
			parent, e := resolve(node.ParentID)
			if e != nil {
				return DrawFrameContext{}, e
			}
			parentFrame = parent.Frame
			active = parent.Active
		}
		fps := d.Canvas.FPS
		for _, source := range d.Sources {
			if source.ID == node.SourceID && source.Canvas != nil {
				fps = source.Canvas.FPS
			}
		}
		active = active && !node.Hidden && parentFrame >= node.From && parentFrame < node.From+node.Duration
		result := DrawFrameContext{Active: active, ParentFrame: parentFrame, FPS: fps}
		if active {
			clock := d.Canvas.FPS
			if node.Kind == "source" {
				clock = fps
			}
			mapped, e := drawingFrameOffset(parentFrame-node.From, node.Speed, clock, d.Canvas.FPS)
			if e != nil {
				return result, e
			}
			result.Frame = node.SourceOffset + mapped
		}
		delete(visiting, id)
		cache[id] = result
		return result, nil
	}
	return resolve(id)
}
