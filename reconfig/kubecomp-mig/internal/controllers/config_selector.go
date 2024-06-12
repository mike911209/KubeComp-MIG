package controllers

import (
	"log"
	"fmt"
)

func (r *ReconfigReconciler) getBestConfig(requestMigSlices map[string]int64, podLocation map[int][]Pod, curConfig string) (string, error) {
	candidateConfigs := r.getBestConfigFilter(requestMigSlices)
	scores := r.getBestConfigScore(candidateConfigs, podLocation, curConfig)

	log.Printf("Scores of config: %v\n", scores)

	var maxScore int
	var maxConfigName string
	for configName, score := range scores {
		if score > maxScore || maxConfigName == "" {
			maxScore = score
			maxConfigName = configName
		}
	}

	if maxConfigName == "" {
		return maxConfigName, fmt.Errorf("Config not found.")
	}
	return maxConfigName, nil
}

func (r *ReconfigReconciler) getBestConfigFilter(requestMigSlices map[string]int64) []string {
	var candidateConfigs []string
	for profileName, migConfig := range r.MigPartedConfig.MigConfigs {
		find := true
		log.Printf("Check profile %s\n", profileName)
		for requestMigSlice, requestMigCnt := range requestMigSlices {
			cnt := int64(0)
			for _, deviceConfig := range migConfig {
				removeString := "nvidia.com/mig-"
				sliceName := requestMigSlice[len(removeString):]
				cnt += int64(deviceConfig.MigDevices[sliceName] * len(deviceConfig.Devices))
			}
			log.Printf("%s: %d\n", requestMigSlice, cnt)
			if cnt < requestMigCnt {
				find = false
				break
			}
		}
		if find {
			candidateConfigs = append(candidateConfigs, profileName)
		}
	}
	return candidateConfigs
}

func (r *ReconfigReconciler) getBestConfigScore(candidateConfigs []string, podLocation map[int][]Pod, curConfig string) map[string]int {
	scores := make(map[string]int)
	for _, config := range candidateConfigs {
		gpuIDs := r.getReconfigGPU(curConfig, config)
		score := 0
		for _, id := range gpuIDs {
			score -= len(podLocation[id])
		}
		scores[config] = score
	}
	return scores
}