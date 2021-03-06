/*
 * Copyleft 2017, Simone Margaritelli <evilsocket at protonmail dot com>
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are met:
 *
 *   * Redistributions of source code must retain the above copyright notice,
 *     this list of conditions and the following disclaimer.
 *   * Redistributions in binary form must reproduce the above copyright
 *     notice, this list of conditions and the following disclaimer in the
 *     documentation and/or other materials provided with the distribution.
 *   * Neither the name of ARM Inject nor the names of its contributors may be used
 *     to endorse or promote products derived from this software without
 *     specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
 * AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE
 * LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
 * CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
 * SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
 * INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
 * CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
 * POSSIBILITY OF SUCH DAMAGE.
 */
package xray

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// This structure contains some runtime statistics.
type Statistics struct {
	// Time the execution started
	Start time.Time
	// Time the execution finished
	Stop time.Time
	// Total duration of the execution
	Total time.Duration
	// Total number of inputs from the wordlist
	Inputs uint64
	// Executions per second
	Eps float64
	// Total number of executions
	Execs uint64
	// Total number of executions with positive results.
	Results uint64
	// % of progress as: ( execs / inputs ) * 100.0
	Progress float64
}

// This is where the main logic goes.
type RunHandler func(line string) interface{}

// This is where positive results are handled.
type ResultHandler func(result interface{})

// The main object.
type Machine struct {
	// Runtime statistics.
	Stats Statistics
	// Number of input consumers.
	consumers uint
	// Dictionary file name.
	filename string
	// Positive results channel.
	output chan interface{}
	// Inputs channel.
	input chan string
	// WaitGroup to stop while the machine is running.
	wait sync.WaitGroup
	// Main logic handler.
	run_handler RunHandler
	// Positive results handler.
	res_handler ResultHandler
}

// Builds a new machine object, if consumers is less or equal than 0, CPU*2 will be used as default value.
func NewMachine(consumers int, filename string, session *Session, run_handler RunHandler, res_handler ResultHandler) *Machine {
	workers := uint(0)
	if consumers <= 0 {
		workers = uint(runtime.NumCPU() * 2)
	} else {
		workers = uint(consumers)
	}

	var stats *Statistics
	if session.Stats != nil && session.Stats.Execs > 0 {
		stats = session.Stats
	} else {
		stats = &Statistics{}
	}

	return &Machine{
		Stats:       *stats,
		consumers:   workers,
		filename:    filename,
		output:      make(chan interface{}),
		input:       make(chan string),
		wait:        sync.WaitGroup{},
		run_handler: run_handler,
		res_handler: res_handler,
	}
}

func (m *Machine) inputConsumer() {
	for in := range m.input {
		atomic.AddUint64(&m.Stats.Execs, 1)

		res := m.run_handler(in)
		if res != nil {
			atomic.AddUint64(&m.Stats.Results, 1)
			m.output <- res
		}
		m.wait.Done()
	}
}

func (m *Machine) outputConsumer() {
	for res := range m.output {
		m.res_handler(res)
	}
}

func (m *Machine) AddInput(input string) {
	m.wait.Add(1)
	m.input <- input
}

// Start the machine.
func (m *Machine) Start() error {
	// start a fixed amount of consumers for inputs
	for i := uint(0); i < m.consumers; i++ {
		go m.inputConsumer()
	}

	// start the output consumer on a goroutine
	go m.outputConsumer()

	// count the inputs we have
	m.Stats.Inputs = 0
	lines, err := LineReader(m.filename, 0)
	if err != nil {
		return err
	}
	for range lines {
		m.Stats.Inputs++
	}

	lines, err = LineReader(m.filename, 0)
	if err != nil {
		return err
	}

	// If the stats have been loaded from a session file.
	if m.Stats.Execs > 0 {
		n := m.Stats.Execs
		for range lines {
			n--
			if n == 0 {
				break
			}
		}
	} else {
		m.Stats.Start = time.Now()
	}

	for line := range lines {
		m.AddInput(line)
	}

	return nil
}

func (m *Machine) UpdateStats() {
	m.Stats.Stop = time.Now()
	m.Stats.Total = m.Stats.Stop.Sub(m.Stats.Start)
	m.Stats.Eps = float64(m.Stats.Execs) / m.Stats.Total.Seconds()
	m.Stats.Progress = (float64(m.Stats.Execs) / float64(m.Stats.Inputs)) * 100.0
}

// Wait for all jobs to be completed.
func (m *Machine) Wait() {
	// wait for everything to be completed
	m.wait.Wait()
	m.UpdateStats()
}
