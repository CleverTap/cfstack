# cfstack - Cloudformaton cli that works!!

## Commands

### Init
```cfstack init --region eu-west-1```

This will create a cfstack-Init stack in the specified region. The stack consists of resources that are required to run cfstack commands:

 - TemplatesS3Bucket : for storing templates during every run
 - SourceS3Bucket : for storing lambda function code or binaries
 
TODO: 

 - Added role that can be used assumed by cloudformation api to manage resources. This way we can run cfstack with minimal access policy


### Diff
```cfstack diff --manifest manifest.json```

This will generate a list of all stacks that have changes and the respective resources. Stacks are defined in manifest.json (see samples/manifest.json ). 
`TemplatePath` can be absolute path or relative to manifest file.

### Deploy
```cfstack deploy --manifest manifest.json```

This will create, update or delete a stack based on definitions in manifest file or changes in stack template