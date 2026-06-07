package logger_test

import (
	"time"

	"github.com/cenk1cenk2/plumber/v6/logger"
	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Formatter", func() {
	It("should format ordered fields and trim messages", func() {
		formatter := &logger.Formatter{
			FieldsOrder:   []string{"context", "status"},
			HideKeys:      true,
			NoColors:      true,
			NoEmptyFields: true,
			TrimMessages:  true,
		}
		entry := &logrus.Entry{
			Data: logrus.Fields{
				"context": "task",
				"empty":   "",
				"status":  "RUN",
			},
			Level:   logrus.InfoLevel,
			Message: "done \n",
			Time:    time.Unix(0, 0),
		}

		output, err := formatter.Format(entry)

		Expect(err).ToNot(HaveOccurred())
		Expect(string(output)).To(Equal("[I] [task] [RUN] done\n"))
	})

	It("should format compact fields with keys", func() {
		formatter := &logger.Formatter{
			FieldsOrder:      []string{"context", "status"},
			NoColors:         true,
			NoFieldsSpace:    true,
			ShowFullLevel:    true,
			NoUppercaseLevel: true,
		}
		entry := &logrus.Entry{
			Data: logrus.Fields{
				"context": "task",
				"status":  "END",
			},
			Level:   logrus.WarnLevel,
			Message: "done",
			Time:    time.Unix(0, 0),
		}

		output, err := formatter.Format(entry)

		Expect(err).ToNot(HaveOccurred())
		Expect(string(output)).To(Equal("[warning][context:task][status:END] done\n"))
	})

	It("should redact configured secrets from messages", func() {
		secrets := []string{"secret-token"}
		formatter := &logger.Formatter{
			NoColors: true,
			Secrets:  &secrets,
		}
		entry := &logrus.Entry{
			Level:   logrus.InfoLevel,
			Message: "using secret-token",
			Time:    time.Unix(0, 0),
		}

		output, err := formatter.Format(entry)

		Expect(err).ToNot(HaveOccurred())
		Expect(string(output)).To(Equal("[I] using [REDACTED]\n"))
	})
})
