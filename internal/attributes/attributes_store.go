package attributes

import (
	"context"

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// SetHostAttributes persists one host attribute.
func SetHostAttributes(hostname, attributeName, attributeValue string) error {
	err := metadata.SetHostAttribute(context.Background(), hostname, attributeName, attributeValue)
	if err != nil {
		return log.Errore(err)
	}
	return nil
}

// GetHostAttributesByMatch reads attributes matching optional regular expressions.
func GetHostAttributesByMatch(hostnameMatch, attributeNameMatch, attributeValueMatch string) ([]domain.HostAttributes, error) {
	rows, err := metadata.ReadHostAttributesByMatch(context.Background(), hostnameMatch, attributeNameMatch, attributeValueMatch)
	if err != nil {
		log.Errore(err)
	}
	return rows, err
}

// GetHostAttribute returns a single attribute for a hostname/name combination.
func GetHostAttribute(hostname, attributeName string) (string, error) {
	rows, err := metadata.ReadHostAttribute(context.Background(), hostname, attributeName)
	if err != nil {
		return "", err
	}
	return hostAttributeValue(rows, hostname, attributeName)
}

func hostAttributeValue(attributes []domain.HostAttributes, hostname, attributeName string) (string, error) {
	if len(attributes) == 0 {
		return "", log.Errorf("No attribute found for %+v, %+v", hostname, attributeName)
	}
	return attributes[0].AttributeValue, nil
}

// SetGeneralAttribute stores an attribute not associated with a specific host.
func SetGeneralAttribute(attributeName, attributeValue string) error {
	if attributeName == "" {
		return nil
	}
	return SetHostAttributes("*", attributeName, attributeValue)
}

// GetGeneralAttribute reads an attribute not associated with a specific host.
func GetGeneralAttribute(attributeName string) (string, error) {
	return GetHostAttribute("*", attributeName)
}

// GetHostAttributesByAttribute reads all host values for an attribute name.
func GetHostAttributesByAttribute(attributeName, valueMatch string) ([]domain.HostAttributes, error) {
	if valueMatch == "" {
		valueMatch = ".?"
	}
	rows, err := metadata.ReadHostAttributesByAttribute(context.Background(), attributeName, valueMatch)
	if err != nil {
		log.Errore(err)
	}
	return rows, err
}
