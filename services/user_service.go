package services

import (
	"github.com/KubeOrchestra/core/database"
	"github.com/KubeOrchestra/core/model"
)

func CreateUser(user *model.User) error {
	result := database.DB.Create(user)
	return result.Error
}

func GetUserByEmail(email string) (*model.User, error) {
	var user model.User
	result := database.DB.Where("email = ?", email).First(&user)
	if result.Error != nil {
		return nil, result.Error
	}
	return &user, nil
}

func GetUserByID(id uint) (*model.User, error) {
	var user model.User
	result := database.DB.First(&user, id)
	if result.Error != nil {
		return nil, result.Error
	}
	return &user, nil
}

func UserExistsByEmail(email string) (bool, error) {
	var count int64
	result := database.DB.Model(&model.User{}).Where("email = ?", email).Count(&count)
	if result.Error != nil {
		return false, result.Error
	}
	return count > 0, nil
}

func UpdateUser(user *model.User) error {
	result := database.DB.Save(user)
	return result.Error
}

func DeleteUser(id uint) error {
	result := database.DB.Delete(&model.User{}, id)
	return result.Error
}
