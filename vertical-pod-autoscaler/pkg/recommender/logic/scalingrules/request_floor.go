package scalingrules

import (
	"bytes"
	"encoding/json"
	"fmt"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const RequestFloorRule = "request-floor"

type requestFloor struct{ minimum float64 }

func init() {
	Register(RequestFloorRule, newRequestFloor)
}

func newRequestFloor(raw []byte) (ScalingRule, error) {
	var parameters struct {
		Minimum string `json:"minimum"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parameters); err != nil {
		return nil, fmt.Errorf("decode parameters: %w", err)
	}
	minimum, err := parsePercentage(parameters.Minimum, "minimum")
	if err != nil {
		return nil, err
	}
	return requestFloor{minimum: minimum}, nil
}

func (r requestFloor) Apply(rec recommendation.SingleResourceRecommendation, originalRequest model.ResourceAmount) recommendation.SingleResourceRecommendation {
	minimum := model.ScaleResource(originalRequest, r.minimum)
	if rec.Target < minimum {
		rec.Target = minimum
	}
	if rec.LowerBound < minimum {
		rec.LowerBound = minimum
	}
	if rec.UpperBound < minimum {
		rec.UpperBound = minimum
	}
	return rec
}
