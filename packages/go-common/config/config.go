// Package config 统一配置加载：环境变量 + configs/*.yaml。
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Load 读取配置。path 为 yaml 文件目录，name 为文件名（不含扩展名）。
// 环境变量优先级最高，自动映射为 key（如 DB_DSN -> db.dsn）。
func Load(path, name string) (*viper.Viper, error) {
	v := viper.New()
	v.AddConfigPath(path)
	v.SetConfigName(name)
	v.SetConfigType("yaml")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置 %s/%s 失败: %w", path, name, err)
	}
	return v, nil
}

// MustLoad 读取失败直接 panic（启动期）。
func MustLoad(path, name string) *viper.Viper {
	v, err := Load(path, name)
	if err != nil {
		panic(err)
	}
	return v
}
