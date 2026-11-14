package sub

import "github.com/alireza0/x-ui/util/random"

func jsonMap(m map[string]interface{}, key string) map[string]interface{} {
	v, _ := m[key].(map[string]interface{})
	return v
}

func jsonString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func jsonFloat(m map[string]interface{}, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func jsonBool(m map[string]interface{}, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func jsonStrings(value interface{}) []string {
	list, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

func firstJSONString(value interface{}) string {
	list := jsonStrings(value)
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

func randomJSONString(value interface{}) string {
	list := jsonStrings(value)
	if len(list) == 0 {
		return ""
	}
	return list[random.Num(len(list))]
}
