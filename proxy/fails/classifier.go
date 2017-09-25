package fails

//go:generate counterfeiter -o fakes/fake_classifier.go --fake-name Classifier . Classifier
type Classifier interface {
	Classify(err error) bool
}

type ClassifierFunc func(err error) bool

func (f ClassifierFunc) Classify(err error) bool { return f(err) }
