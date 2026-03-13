import { StatusObject, WriteObject } from '@grpc/grpc-js/build/src/call-interface';
import { Filter, FilterFactory } from '@grpc/grpc-js/build/src/filter';
import { Metadata } from '@grpc/grpc-js/build/src/metadata';
export declare class FilterStack implements Filter {
    private readonly filters;
    constructor(filters: Filter[]);
    sendMetadata(metadata: Promise<Metadata>): Promise<Metadata>;
    receiveMetadata(metadata: Metadata): Metadata;
    sendMessage(message: Promise<WriteObject>): Promise<WriteObject>;
    receiveMessage(message: Promise<Buffer>): Promise<Buffer>;
    receiveTrailers(status: StatusObject): StatusObject;
    push(filters: Filter[]): void;
    getFilters(): Filter[];
}
export declare class FilterStackFactory implements FilterFactory<FilterStack> {
    private readonly factories;
    constructor(factories: Array<FilterFactory<Filter>>);
    push(filterFactories: FilterFactory<Filter>[]): void;
    clone(): FilterStackFactory;
    createFilter(): FilterStack;
}
