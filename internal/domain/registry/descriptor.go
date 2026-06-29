package registry

type Descriptor struct {
	MediaType string
	Digest    Digest
	Size      int64
}

func (d Descriptor) Validate() error {
	if err := d.Digest.Validate(); err != nil {
		return err
	}

	if d.Size < 0 {
		return NewValidationError("descriptor size must be zero or positive")
	}

	return nil
}
