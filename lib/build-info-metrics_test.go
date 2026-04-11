// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	stdtime "time"

	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/bborbe/agent/lib"
)

var _ = Describe("BuildInfoMetrics", func() {
	var metrics lib.BuildInfoMetrics

	BeforeEach(func() {
		metrics = lib.NewBuildInfoMetrics()
	})

	readBuildInfoValue := func() float64 {
		mfs, err := prometheus.DefaultGatherer.Gather()
		Expect(err).NotTo(HaveOccurred())
		for _, mf := range mfs {
			if mf.GetName() == "agent_build_info" {
				metricList := mf.GetMetric()
				if len(metricList) == 0 {
					return 0
				}
				return metricList[0].GetGauge().GetValue()
			}
		}
		return -1 // not found
	}

	It("registers agent_build_info in the default prometheus registry", func() {
		Expect(readBuildInfoValue()).NotTo(Equal(float64(-1)))
	})

	It("is a no-op when given a nil DateTime", func() {
		before := readBuildInfoValue()
		Expect(func() { metrics.SetBuildInfo(nil) }).NotTo(Panic())
		Expect(readBuildInfoValue()).To(Equal(before))
	})

	It("sets the gauge to the DateTime's unix timestamp", func() {
		dt := libtime.DateTime(stdtime.Unix(1234567890, 0))
		metrics.SetBuildInfo(&dt)
		Expect(readBuildInfoValue()).To(Equal(float64(1234567890)))
	})

	It("exposes the correct metric type (gauge) via prometheus client_model", func() {
		mfs, err := prometheus.DefaultGatherer.Gather()
		Expect(err).NotTo(HaveOccurred())
		for _, mf := range mfs {
			if mf.GetName() == "agent_build_info" {
				Expect(mf.GetType()).To(Equal(dto.MetricType_GAUGE))
				return
			}
		}
		Fail("agent_build_info metric not found")
	})
})
