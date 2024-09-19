# Dispatcher Manual

## 1. Deploy dispatcher
* In ibm128 : `cd ~/Dispatcher`
* `$ make deploy`

## 2. Reach Dispatcher  nd setup 

* Open another terminal , then : `$ make forward`
* Export your hugging face account access token : `$ export HF_TOKEN=<your token>`

## 3. Send test API request to Dispatcher
* `$ make test`
## 4. Send Customize request to Dispatcher
* Make sure you done step 1. and 2.
* <> are things you can change:
```
curl -X POST http://localhost:8080 \
		-H "Host: dispatcher.default.127.0.0.1.nip.io" \
		-H "Content-Type: application/json" \
		-d '{"token":"<your input query to model>","par":{"<parameter1>":<value1>,"<parameter1>":<value2>,...},"env": {"MODEL_ID":"<model ids listed in https://hf.co/models , ex. meta-llama/Meta-Llama-3.1-8B>", "<env2>":<value2>,....,"HF_TOKEN":"$(HF_TOKEN)"}}'

```
* Reference for paramters: https://huggingface.co/docs/transformers/main_classes/text_generation
* Reference for envs : https://huggingface.co/docs/text-generation-inference/main/en/reference/launcher



