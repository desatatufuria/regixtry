package regixtry

import "maps"

type Manifest struct {
	MediaType   string
	Digest      Digest
	Size        int64
	Payload     []byte
	Subject     *Descriptor
	Config      *Descriptor
	Layers      []Descriptor
	Annotations map[string]string
}

func NewManifest(mediaType string, payload []byte, config *Descriptor, layers []Descriptor, subject *Descriptor, annotations map[string]string) (Manifest, error) {
	if mediaType == "" {
		return Manifest{}, NewInvalidManifestError("media type is required")
	}

	if config != nil {
		if err := config.Validate(); err != nil {
			return Manifest{}, err
		}
	}

	if subject != nil {
		if err := subject.Validate(); err != nil {
			return Manifest{}, err
		}
	}

	validatedLayers := make([]Descriptor, len(layers))
	for index, layer := range layers {
		if err := layer.Validate(); err != nil {
			return Manifest{}, err
		}
		validatedLayers[index] = layer
	}

	storedPayload := append([]byte(nil), payload...)

	return Manifest{
		MediaType:   mediaType,
		Digest:      DigestFromBytes(storedPayload),
		Size:        int64(len(storedPayload)),
		Payload:     storedPayload,
		Subject:     cloneDescriptor(subject),
		Config:      cloneDescriptor(config),
		Layers:      validatedLayers,
		Annotations: maps.Clone(annotations),
	}, nil
}

func (m Manifest) Validate() error {
	if m.MediaType == "" {
		return NewInvalidManifestError("media type is required")
	}

	if err := m.Digest.Validate(); err != nil {
		return err
	}

	if m.Size != int64(len(m.Payload)) {
		return NewInvalidManifestError("manifest size does not match payload length")
	}

	if m.Digest != DigestFromBytes(m.Payload) {
		return NewDigestMismatchError(m.Digest, DigestFromBytes(m.Payload))
	}

	if m.Config != nil {
		if err := m.Config.Validate(); err != nil {
			return err
		}
	}

	if m.Subject != nil {
		if err := m.Subject.Validate(); err != nil {
			return err
		}
	}

	for _, layer := range m.Layers {
		if err := layer.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (m Manifest) References() []Descriptor {
	references := make([]Descriptor, 0, len(m.Layers)+2)
	if m.Config != nil {
		references = append(references, *m.Config)
	}

	references = append(references, m.Layers...)

	if m.Subject != nil {
		references = append(references, *m.Subject)
	}

	return references
}

func cloneDescriptor(value *Descriptor) *Descriptor {
	if value == nil {
		return nil
	}

	cloned := *value
	return &cloned
}
