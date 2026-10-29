package util

import (
	"math"
	"sort"
	"time"
)

type Observation interface {
	Predict_SMA(N, input int) float64
	Predict_EMA(N, input int) float64
	Add(value float64, time time.Time)
	Subtract(value float64, time time.Time)
	Merge(other Observation)
	IsEmpty() bool
	Equals(other Observation) bool
}

type Item struct {
	value float64
	time  time.Time
}

type Items []Item

func (e Items) Len() int {
	return len(e)
}

func (e Items) Less(i, j int) bool {
	return e[i].time.Before(e[j].time)
}

func (e Items) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

//returns a new instance of Observation interface
func NewObservation() Observation {
	return &observation{
		bucket:    Items{},
		beginTime: time.Time{},
		endTime:   time.Time{},
	}
}

type observation struct {
	bucket    Items
	beginTime time.Time
	endTime   time.Time
}

func (o *observation) Number() int {
	return len(o.bucket)
}

func (o *observation) Clear() {
	o.beginTime = time.Time{}
	o.endTime = time.Time{}
	o.bucket = nil
}

func (o *observation) Add(value float64, time time.Time) {
	item := Item{value, time}
	o.bucket = append(o.bucket, item)
	sort.Sort(o.bucket)

	o.beginTime = minTime(o.beginTime, time)
	o.endTime = maxTime(o.endTime, time)
}

func (o *observation) Subtract(value float64, time time.Time) {
	if o.IsEmpty() {
		o.Clear()
	}
	item := Item{value, time}
	for i := 0; i < len(o.bucket); i++ {
		if o.bucket[i] == item {
			o.bucket = append(o.bucket[:i], o.bucket[i+1:]...)
			i--
		}
	}
	if len(o.bucket) != 0 {
		o.beginTime = o.bucket[0].time
		o.endTime = o.bucket[len(o.bucket)-1].time
	}
}

func (o *observation) Merge(other Observation) {
	another := other.(*observation)
	// o.bucket = append(o.bucket, another.bucket...)
	// sort.Sort(o.bucket)
	sort.Sort(o.bucket)
	sort.Sort(another.bucket)
	o.bucket = mergeArr(o.bucket, another.bucket)
	if len(o.bucket) != 0 {
		o.beginTime = o.bucket[0].time
		o.endTime = o.bucket[len(o.bucket)-1].time
	}
}

func (o *observation) IsEmpty() bool {
	return len(o.bucket) == 0
}

func (o *observation) Equals(other Observation) bool {
	another := other.(*observation)
	return isEqual(o.bucket, another.bucket)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func mergeArr(a, b []Item) []Item {
	al := len(a)
	bl := len(b)
	cl := al + bl
	c := make([]Item, cl)
	ai, bi, ci := 0, 0, 0

	for ai < al && bi < bl {
		if a[ai].time.Before(b[bi].time) {
			c[ci] = a[ai]
			ci++
			ai++
		} else {
			c[ci] = b[bi]
			ci++
			bi++
		}
	}
	for ai < al {
		c[ci] = a[ai]
		ci++
		ai++
	}
	for bi < bl {
		c[ci] = b[bi]
		ci++
		bi++
	}
	return c
}

func isEqual(a, b []Item) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

//N is the number of measures to average over
//Simple Moving Average
func (o *observation) Predict_SMA(N, input int) float64 {
	if o.IsEmpty() {
		return 0.0
	}
	var avg []float64
	length := len(o.bucket)
	if length >= input-1+N {
		for i := 0; i < input; i++ {
			avg = append(avg, average(o.bucket[length-i-N:length-i]))
		}
		m := (avg[0] - avg[len(avg)-1]) / float64(len(avg)-1)                                                      //m = (l(i)-l(i-q))/q
		return math.Max(1.05*average(o.bucket[length-N:length]), m*float64(0.9*float64(len(avg)))+avg[len(avg)-1]) //m*[(i+k)-(i-q)]+l(i-q), k = 2q
	} else if length < N {
		return average(o.bucket)
	} else { //N <= length < input-1+N
		for i := 0; i < length-N+1; i++ {
			avg = append(avg, average(o.bucket[length-i-N:length-i]))
		}
		m := (avg[0] - avg[len(avg)-1]) / float64(len(avg)-1)
		return math.Max(1.05*average(o.bucket[length-N:length]), m*float64(0.9*float64(len(avg)))+avg[len(avg)-1]) //m*[(i+k)-(i-q)]+l(i-q), k = 2q
	}
}

func average(items []Item) float64 {
	sum := 0.0
	if len(items) == 0 {
		return 0.0
	} else {
		for _, item := range items {
			sum += item.value
		}
		return float64(sum) / float64(len(items))
	}
}

//N is the number of measures to average over
//Exponential Moving Average
func (o *observation) Predict_EMA(N, input int) float64 {
	if o.IsEmpty() {
		return 0.0
	}
	var avg []float64
	length := len(o.bucket)
	if length >= N+input-1 {
		first := average(o.bucket[length-N-input+1 : length-input+1])
		avg = append(avg, first)
		for i := length - input + 1; i < length; i++ {
			tmp := exponentialAvg(o.bucket[i].value, avg[len(avg)-1], N)
			avg = append(avg, tmp)
		}
		m := (avg[len(avg)-1] - avg[0]) / float64(len(avg)-1)
		return math.Max(1.05*avg[len(avg)-1], m*float64(0.9*float64(len(avg)))+avg[0]) //m*[(i+k)-(i-q)]+l(i-q), k = 2q
	} else {
		return average(o.bucket)
	}
}

func exponentialAvg(value float64, oldValue float64, N int) float64 {
	alpha := float64(2) / float64(N+1)
	return alpha*value + (1-alpha)*oldValue
}
