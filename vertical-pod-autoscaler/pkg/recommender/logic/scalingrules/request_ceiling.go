package scalingrules

import (
	"bytes"
	"encoding/json"
	"fmt"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const RequestCeilingRule = "request-ceiling"

type requestCeiling struct{ maximum float64 }

func init() {
	Register(RequestCeilingRule, newRequestCeiling)
}

func newRequestCeiling(raw []byte) (ScalingRule, error) {
	var parameters struct {
		Maximum string `json:"maximum"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parameters); err != nil {
		return nil, fmt.Errorf("decode parameters: %w", err)
	}
	maximum, err := parsePercentage(parameters.Maximum, "maximum")
	if err != nil {
		return nil, err
	}
	return requestCeiling{maximum: maximum}, nil
}

func (r requestCeiling) Apply(rec recommendation.SingleResourceRecommendation, originalRequest model.ResourceAmount) recommendation.SingleResourceRecommendation {
	maximum := model.ScaleResource(originalRequest, r.maximum)
	if rec.Target > maximum {
		rec.Target = maximum
	}
	if rec.LowerBound > maximum {
		rec.LowerBound = maximum
	}
	if rec.UpperBound > maximum {
		rec.UpperBound = maximum
	}
	return rec
}
