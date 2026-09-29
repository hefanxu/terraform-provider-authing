terraform {
  required_providers {
    authing = {
      source  = "authing/authing"
      version = "~> 1.0.0"
    }
  }
}

provider "authing" {
  access_key_id     = "YOUR_AUTHING_USERPOOL_ID"
  access_key_secret = "YOUR_AUTHING_USERPOOL_SECRET"
  # host            = "https://api.authing.cn"
}
