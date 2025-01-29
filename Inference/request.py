import requests

url = "http://localhost:8080/request"

data = {
    "request": "Once upon a time"
}

try:
    response = requests.post(url, json=data)
    if response.status_code == 200:
        print("Response received:")
        print(response.json())
    else:
        print(f"Error: {response.status_code}")
        print(response.text)
except Exception as e:
    print(f"Error occurred: {e}")