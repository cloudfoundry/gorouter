package grouper_test

import (
	"github.com/tedsuo/ifrit/grouper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Members", func() {
	Describe("Validate", func() {
		type duplicateNameExample struct {
			memberNames   []string
			expectedError error
		}

		var testInput []duplicateNameExample

		BeforeEach(func() {
			testInput = []duplicateNameExample{
				{[]string{"foo", "foo"}, grouper.ErrDuplicateNames{[]string{"foo"}}},
				{[]string{"foo", "bar", "foo", "bar", "none"}, grouper.ErrDuplicateNames{[]string{"foo", "bar"}}},
				{[]string{"foo", "bar"}, nil},
				{[]string{"f", "foo", "fooo"}, nil},
			}
		})

		It("returns any found duplicate names", func() {
			for _, example := range testInput {
				members := grouper.Members{}
				for _, name := range example.memberNames {
					members = append(members, grouper.Member{Name: name, Runner: nil})
				}
				err := members.Validate()
				if err == nil {
					Ω(example.expectedError).Should(BeNil())
				} else {
					Ω(err).Should(Equal(example.expectedError))
				}

			}
		})
	})
})
